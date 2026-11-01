import { $, api, formatDuration, formatDistance, toast, scoreColor } from './util.js';
import { loadGoogleMaps, reverseGeocode, circleIcon } from './maps.js';
import { findRandomPanorama } from './random.js';

const WORLD = { center: { lat: 20, lng: 0 }, zoom: 1 };

let pano = null;
let guessMap = null;
let guessMarker = null;
let resultMap = null;
let resultOverlays = [];

let game = null;     // { id, location, startedAt, guessed, feedbackSent }
let timerId = null;
let token = 0;       // 진행 중인 비동기 시작 작업 취소용

function initOnce() {
  if (pano) return;

  // 스트리트뷰 (지도와 연동되지 않음)
  pano = new google.maps.StreetViewPanorama($('game-pano'), {
    addressControl: false,
    showRoadLabels: false,
    fullscreenControl: false,
    motionTracking: false,
    motionTrackingControl: false,
    enableCloseButton: false,
    linksControl: true,
    panControl: true,
    zoomControl: true,
    visible: false,
  });

  // 좌측 하단 floating 미니맵
  guessMap = new google.maps.Map($('game-map'), {
    ...WORLD,
    disableDefaultUI: true,
    zoomControl: true,
    clickableIcons: false,
    gestureHandling: 'greedy',
    draggableCursor: 'crosshair',
    streetViewControl: false,
  });
  guessMap.addListener('click', (e) => {
    if (!game || game.guessed) return;
    if (!guessMarker) {
      guessMarker = new google.maps.Marker({ map: guessMap, position: e.latLng, icon: circleIcon('#ef4444', 8) });
    } else {
      guessMarker.setPosition(e.latLng);
      guessMarker.setMap(guessMap);
    }
    const btn = $('guess-btn');
    btn.disabled = false;
    btn.textContent = 'Guess!';
  });

  resultMap = new google.maps.Map($('result-map'), {
    ...WORLD,
    disableDefaultUI: true,
    zoomControl: true,
    clickableIcons: false,
    gestureHandling: 'greedy',
  });

  $('guess-btn').addEventListener('click', submitGuess);
  $('game-quit').addEventListener('click', quitGame);
  $('game-return').addEventListener('click', returnToStart);
  $('game-retry').addEventListener('click', () => startNewGame());
  $('result-next').addEventListener('click', () => startNewGame());
  $('guess-pin').addEventListener('click', () => {
    const panel = $('guess-panel');
    panel.classList.toggle('pinned');
    $('guess-pin').classList.toggle('active', panel.classList.contains('pinned'));
  });
  $('guess-expand').addEventListener('click', () => {
    const expanded = $('guess-panel').classList.toggle('expanded');
    $('guess-expand').classList.toggle('active', expanded);
  });
  $('guess-panel').addEventListener('mouseleave', () => {
    const panel = $('guess-panel');
    if (panel.classList.contains('pinned')) return;
    panel.classList.remove('expanded');
    $('guess-expand').classList.remove('active');
  });
  document.querySelectorAll('#result-overlay .fb-btn').forEach((b) =>
    b.addEventListener('click', () => sendFeedback(Number(b.dataset.value))));

  document.addEventListener('keydown', (e) => {
    if ($('view-game').classList.contains('hidden')) return;
    if (e.code === 'Space' && !$('guess-btn').disabled && game && !game.guessed) {
      e.preventDefault();
      submitGuess();
    }
  });
}

export async function enterGame() {
  await startNewGame();
}

export function leaveGame() {
  token++;
  stopTimer();
  abandon();
  game = null;
}

function abandon() {
  if (game && !game.guessed) {
    // keepalive: 페이지를 닫는 중에도 요청이 전송되도록
    fetch(`/api/games/${game.id}`, { method: 'DELETE', keepalive: true }).catch(() => {});
  }
}

function setLoading(text, isError = false) {
  $('game-loading').classList.remove('hidden');
  $('game-loading').classList.toggle('error', isError);
  $('game-loading-text').textContent = text;
  $('game-loading-actions').classList.toggle('hidden', !isError);
  $('game-loading').querySelector('.spinner').classList.toggle('hidden', isError);
}

function resetUI() {
  stopTimer();
  $('game-timer').textContent = formatDuration(0);
  $('result-overlay').classList.add('hidden');
  const btn = $('guess-btn');
  btn.disabled = true;
  btn.textContent = '지도에 핀을 찍으세요';
  if (guessMarker) guessMarker.setMap(null);
  if (guessMap) { guessMap.setCenter(WORLD.center); guessMap.setZoom(WORLD.zoom); }
  resultOverlays.forEach((o) => o.setMap(null));
  resultOverlays = [];
}

async function startNewGame() {
  const my = ++token;
  const cancelled = () => my !== token;
  abandon();
  game = null;
  resetUI();
  setLoading('위치를 준비하는 중...');

  try {
    await loadGoogleMaps();
    if (cancelled()) return;
    initOnce();
    resetUI();
    pano.setVisible(false);

    const draw = await api('GET', '/api/games/draw');
    if (cancelled()) return;

    let body;
    if (draw.source === 'admin') {
      body = { locationId: draw.location.id };
    } else {
      const found = await findRandomPanorama(
        (n) => { if (!cancelled()) setLoading(`스트리트뷰 위치를 찾는 중... (${n})`); },
        60, cancelled);
      if (cancelled()) return;
      const address = await reverseGeocode(found.lat, found.lng, found.description);
      if (cancelled()) return;
      body = { location: { lat: found.lat, lng: found.lng, panoId: found.panoId, heading: found.heading, address } };
    }

    const created = await api('POST', '/api/games', body);
    if (cancelled()) {
      api('DELETE', `/api/games/${created.gameId}`).catch(() => {});
      return;
    }

    game = { id: created.gameId, location: created.location, guessed: false, feedbackSent: false };
    showStart();
    $('game-loading').classList.add('hidden');
    game.startedAt = performance.now();
    startTimer();
  } catch (e) {
    if (cancelled() || e.message === 'cancelled') return;
    console.error(e);
    setLoading(e.message || '게임을 시작하지 못했습니다.', true);
  }
}

function showStart() {
  const loc = game.location;
  if (loc.panoId) pano.setPano(loc.panoId);
  else pano.setPosition({ lat: loc.lat, lng: loc.lng });
  pano.setPov({ heading: loc.heading || 0, pitch: 0 });
  pano.setZoom(0);
  pano.setVisible(true);
}

function returnToStart() {
  if (game) showStart();
}

function startTimer() {
  stopTimer();
  const tick = () => {
    if (!game) return;
    $('game-timer').textContent = formatDuration((performance.now() - game.startedAt) / 1000);
  };
  tick();
  timerId = setInterval(tick, 250);
}

function stopTimer() {
  if (timerId) clearInterval(timerId);
  timerId = null;
}

async function submitGuess() {
  if (!game || game.guessed || !guessMarker) return;
  const btn = $('guess-btn');
  btn.disabled = true;
  const pos = guessMarker.getPosition();
  try {
    const res = await api('POST', `/api/games/${game.id}/guess`, { lat: pos.lat(), lng: pos.lng() });
    game.guessed = true;
    stopTimer();
    $('game-timer').textContent = formatDuration(res.durationS);
    showResult(res);
  } catch (e) {
    btn.disabled = false;
    toast(e.message, 'error');
  }
}

function showResult(res) {
  $('result-score').textContent = res.score.toFixed(1);
  const bar = $('result-score-bar');
  bar.style.width = `${res.score}%`;
  bar.style.background = scoreColor(res.score);
  $('result-distance').textContent = formatDistance(res.distanceM);
  $('result-time').textContent = formatDuration(res.durationS);
  $('result-dp').textContent = `-${res.distancePenalty.toFixed(1)}점`;
  $('result-tp').textContent = `-${res.timePenalty.toFixed(1)}점`;
  $('result-address').textContent = res.location.address ? `📍 ${res.location.address}` : '';

  document.querySelectorAll('#result-overlay .fb-btn').forEach((b) => { b.disabled = false; b.classList.remove('chosen'); });
  $('feedback-done').classList.add('hidden');

  $('result-overlay').classList.remove('hidden');

  const actual = { lat: res.location.lat, lng: res.location.lng };
  const guess = res.guess;
  resultOverlays.push(
    new google.maps.Polyline({
      map: resultMap,
      path: [actual, guess],
      geodesic: true,
      strokeOpacity: 0,
      icons: [{ icon: { path: 'M 0,-1 0,1', strokeOpacity: 1, strokeColor: '#111827', scale: 3 }, offset: '0', repeat: '14px' }],
    }),
    new google.maps.Marker({ map: resultMap, position: actual, icon: circleIcon('#22c55e', 10), title: '정답 위치', zIndex: 2 }),
    new google.maps.Marker({ map: resultMap, position: guess, icon: circleIcon('#ef4444', 8), title: '내 추측', zIndex: 1 }),
  );
  // 오버레이가 표시된 뒤 크기가 잡히므로 다음 프레임에 맞춤
  requestAnimationFrame(() => {
    if (res.distanceM < 50) {
      resultMap.setCenter(actual);
      resultMap.setZoom(17);
    } else {
      const b = new google.maps.LatLngBounds();
      b.extend(actual);
      b.extend(guess);
      resultMap.fitBounds(b, 60);
    }
  });
}

async function sendFeedback(value) {
  if (!game || !game.guessed || game.feedbackSent) return;
  const btns = document.querySelectorAll('#result-overlay .fb-btn');
  btns.forEach((b) => { b.disabled = true; });
  try {
    await api('POST', `/api/games/${game.id}/feedback`, { value });
    game.feedbackSent = true;
    btns.forEach((b) => b.classList.toggle('chosen', Number(b.dataset.value) === value));
    $('feedback-done').classList.remove('hidden');
  } catch (e) {
    btns.forEach((b) => { b.disabled = false; });
    toast(e.message, 'error');
  }
}

function quitGame() {
  if (game && !game.guessed && !confirm('게임을 종료하고 홈으로 돌아갈까요? 진행 중인 게임은 기록되지 않습니다.')) return;
  location.hash = '#/';
}
