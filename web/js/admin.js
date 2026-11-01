import { $, api, toast, escapeHtml, formatDate } from './util.js';
import { loadGoogleMaps, panoRequest, reverseGeocode, circleIcon } from './maps.js';

let pano = null;
let map = null;
let sv = null;
let pinMarker = null;
let pinned = null;          // { lat, lng, panoId, heading }
let listSource = 'admin';
let locations = [];
let locationMarkers = [];
let showMarkers = true;
let clickSeq = 0;

function initOnce() {
  if (map) return;
  sv = new google.maps.StreetViewService();

  pano = new google.maps.StreetViewPanorama($('admin-pano'), {
    visible: false,
    addressControl: true,
    fullscreenControl: false,
    motionTracking: false,
    motionTrackingControl: false,
    enableCloseButton: false,
  });

  // 지도와 파노라마는 별개 객체: 파노라마 이동은 핀/지도에 반영되지 않음
  map = new google.maps.Map($('admin-map'), {
    center: { lat: 20, lng: 0 },
    zoom: 2,
    streetViewControl: false,
    mapTypeControl: true,
    fullscreenControl: false,
    clickableIcons: false,
    gestureHandling: 'greedy',
    draggableCursor: 'crosshair',
  });
  new google.maps.StreetViewCoverageLayer().setMap(map);
  map.addListener('click', (e) => onMapClick(e.latLng));

  $('admin-reset').addEventListener('click', resetToPin);
  $('admin-save').addEventListener('click', savePin);
  $('admin-toggle-markers').addEventListener('click', () => {
    showMarkers = !showMarkers;
    $('admin-toggle-markers').textContent = `등록 위치 표시: ${showMarkers ? '켜짐' : '꺼짐'}`;
    renderMarkers();
  });
  document.querySelectorAll('#view-admin .seg-btn').forEach((b) =>
    b.addEventListener('click', () => {
      document.querySelectorAll('#view-admin .seg-btn').forEach((x) => x.classList.toggle('active', x === b));
      listSource = b.dataset.source;
      refreshList();
    }));
  $('admin-list-body').addEventListener('click', onListClick);
}

export async function enterAdmin() {
  try {
    await loadGoogleMaps();
    initOnce();
    refreshList();
  } catch (e) {
    toast(e.message, 'error');
  }
}

export function leaveAdmin() {}

/** 줌 레벨에 따라 클릭 지점 주변 탐색 반경(약 15px) 계산 */
function searchRadius(latLng) {
  const metersPerPx = (156543.03392 * Math.cos(latLng.lat() * Math.PI / 180)) / 2 ** map.getZoom();
  return Math.min(50000, Math.max(25, metersPerPx * 15));
}

async function onMapClick(latLng) {
  const seq = ++clickSeq;
  try {
    const { data } = await sv.getPanorama(panoRequest(latLng, searchRadius(latLng)));
    if (seq !== clickSeq) return;
    const loc = data.location;
    setPin({ lat: loc.latLng.lat(), lng: loc.latLng.lng(), panoId: loc.pano, heading: 0 }, loc.description);
  } catch {
    if (seq === clickSeq) toast('클릭한 지점 근처에 스트리트뷰가 없습니다. 파란색 선/점을 클릭하세요.', 'warn');
  }
}

async function setPin(p, fallbackAddress = '') {
  pinned = p;
  if (!pinMarker) {
    pinMarker = new google.maps.Marker({ map, position: p, zIndex: 10, title: '고정된 핀' });
  } else {
    pinMarker.setPosition(p);
  }
  resetToPin();
  $('admin-pano-empty').classList.add('hidden');
  $('admin-reset').disabled = false;
  $('admin-save').disabled = false;
  const input = $('admin-address');
  input.disabled = false;
  input.value = '주소 확인 중...';
  $('admin-pin-info').innerHTML = `📍 핀: <b>${p.lat.toFixed(6)}, ${p.lng.toFixed(6)}</b> <span class="muted">(pano ${escapeHtml(p.panoId)})</span>`;
  const addr = await reverseGeocode(p.lat, p.lng, fallbackAddress);
  if (pinned === p) input.value = addr;
}

function resetToPin() {
  if (!pinned) return;
  pano.setPano(pinned.panoId);
  pano.setPov({ heading: pinned.heading || 0, pitch: 0 });
  pano.setZoom(0);
  pano.setVisible(true);
}

async function savePin() {
  if (!pinned) return;
  const btn = $('admin-save');
  btn.disabled = true;
  try {
    // 출제 시 바라보는 방향은 현재 스트리트뷰 시점을 사용
    const heading = pano.getPano() === pinned.panoId ? pano.getPov().heading : pinned.heading;
    const address = $('admin-address').value.trim();
    await api('POST', '/api/admin/locations', { ...pinned, heading, address });
    toast('관리자 위치로 추가되었습니다.', 'success');
    if (listSource === 'random') {
      document.querySelector('#view-admin .seg-btn[data-source="admin"]').click();
    } else {
      refreshList();
    }
  } catch (e) {
    toast(e.message, 'error');
  } finally {
    btn.disabled = false;
  }
}

async function refreshList() {
  try {
    const qs = listSource ? `?source=${listSource}` : '';
    const data = await api('GET', `/api/admin/locations${qs}`);
    locations = data.locations;
    renderList();
    renderMarkers();
  } catch (e) {
    toast(e.message, 'error');
  }
}

function renderList() {
  $('admin-count').textContent = `${locations.length}개`;
  const body = $('admin-list-body');
  if (locations.length === 0) {
    body.innerHTML = `<tr><td colspan="6" class="empty">등록된 위치가 없습니다</td></tr>`;
    return;
  }
  body.innerHTML = locations.map((l) => {
    const fbClass = l.feedback > 0 ? 'pos' : l.feedback < 0 ? 'neg' : '';
    return `<tr>
      <td>${l.id}</td>
      <td class="addr" title="${escapeHtml(formatDate(l.createdAt))}">${escapeHtml(l.address || `${l.lat.toFixed(4)}, ${l.lng.toFixed(4)}`)}</td>
      <td><span class="badge ${l.source}">${l.source === 'admin' ? '관리자' : '랜덤'}</span></td>
      <td class="${fbClass}">${l.feedback > 0 ? '+' : ''}${l.feedback}</td>
      <td>${l.plays ?? 0}</td>
      <td class="actions">
        <button class="icon-btn" data-act="view" data-id="${l.id}" title="이 위치 보기">👁</button>
        <button class="icon-btn" data-act="delete" data-id="${l.id}" title="삭제">🗑</button>
      </td>
    </tr>`;
  }).join('');
}

function renderMarkers() {
  locationMarkers.forEach((m) => m.setMap(null));
  locationMarkers = [];
  if (!showMarkers) return;
  locationMarkers = locations.map((l) => {
    const m = new google.maps.Marker({
      map,
      position: { lat: l.lat, lng: l.lng },
      icon: circleIcon(l.source === 'admin' ? '#8b5cf6' : '#f59e0b', 5),
      title: `#${l.id} ${l.address}`,
    });
    m.addListener('click', () => viewLocation(l));
    return m;
  });
}

function viewLocation(l) {
  map.panTo({ lat: l.lat, lng: l.lng });
  if (map.getZoom() < 14) map.setZoom(14);
  setPin({ lat: l.lat, lng: l.lng, panoId: l.panoId, heading: l.heading }, l.address);
}

async function onListClick(e) {
  const btn = e.target.closest('button[data-act]');
  if (!btn) return;
  const id = Number(btn.dataset.id);
  const loc = locations.find((l) => l.id === id);
  if (!loc) return;
  if (btn.dataset.act === 'view') {
    viewLocation(loc);
  } else if (btn.dataset.act === 'delete') {
    if (!confirm(`#${id} 위치를 삭제할까요?\n(기존 게임 기록은 유지되며 위치 정보만 비워집니다)`)) return;
    try {
      await api('DELETE', `/api/admin/locations/${id}`);
      toast('삭제되었습니다.', 'success');
      refreshList();
    } catch (err) {
      toast(err.message, 'error');
    }
  }
}
