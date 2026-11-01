import { api, toast } from './util.js';

let configPromise = null;
let loadPromise = null;
export let authFailed = false;

export function getConfig() {
  if (!configPromise) {
    configPromise = api('GET', '/api/config').catch((e) => { configPromise = null; throw e; });
  }
  return configPromise;
}

/** Google Maps JS API 를 한 번만 로드하고 필요한 라이브러리를 google.maps 네임스페이스에 준비한다. */
export function loadGoogleMaps() {
  if (loadPromise) return loadPromise;
  loadPromise = (async () => {
    const cfg = await getConfig();
    await new Promise((resolve, reject) => {
      window.__gmapsReady = resolve;
      window.gm_authFailure = () => {
        authFailed = true;
        toast('Google Maps API 인증에 실패했습니다. API 키와 활성화된 API를 확인하세요.', 'error');
      };
      const params = new URLSearchParams({
        key: cfg.googleMapsApiKey || '',
        v: 'weekly',
        language: 'ko',
        loading: 'async',
        callback: '__gmapsReady',
      });
      const s = document.createElement('script');
      s.src = `https://maps.googleapis.com/maps/api/js?${params}`;
      s.async = true;
      s.onerror = () => reject(new Error('Google Maps 스크립트를 불러오지 못했습니다.'));
      document.head.appendChild(s);
    });
    await Promise.all(['core', 'maps', 'streetView', 'geocoding', 'marker'].map((lib) => google.maps.importLibrary(lib)));
  })();
  loadPromise.catch(() => { loadPromise = null; });
  return loadPromise;
}

/** 게임/관리자 모두 동일 기준(Google 공식 촬영 파노라마)으로 스트리트뷰를 찾는다. */
export function panoRequest(location, radius) {
  const req = { location, radius, preference: google.maps.StreetViewPreference.NEAREST };
  if (google.maps.StreetViewSource?.GOOGLE) req.sources = [google.maps.StreetViewSource.GOOGLE];
  else req.source = google.maps.StreetViewSource.OUTDOOR;
  return req;
}

export async function reverseGeocode(lat, lng, fallback = '') {
  try {
    const geocoder = new google.maps.Geocoder();
    const res = await Promise.race([
      geocoder.geocode({ location: { lat, lng } }),
      new Promise((_, rej) => setTimeout(() => rej(new Error('timeout')), 5000)),
    ]);
    const addr = res?.results?.[0]?.formatted_address;
    if (addr) return addr;
  } catch { /* Geocoding API 미활성화 등 → fallback */ }
  return fallback || `${lat.toFixed(5)}, ${lng.toFixed(5)}`;
}

export function circleIcon(color, scale = 8) {
  return {
    path: google.maps.SymbolPath.CIRCLE,
    scale,
    fillColor: color,
    fillOpacity: 1,
    strokeColor: '#ffffff',
    strokeWeight: 2,
  };
}
