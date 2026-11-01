import { panoRequest, authFailed } from './maps.js';

// 스트리트뷰 커버리지가 있는 지역의 대략적 경계 [남, 북, 서, 동, 가중치].
// 완전 무작위 좌표는 대부분 바다/오지라 실패하므로, GeoGuessr 류 서비스처럼 커버리지 지역에 가중치를 두고 뽑는다.
const REGIONS = [
  [32, 49, -124, -104, 10],    // 미국 서부
  [30, 47, -104, -70, 12],     // 미국 중·동부
  [25, 31, -98, -80, 3],       // 미국 남부 해안
  [58, 65, -152, -145, 0.5],   // 알래스카
  [43, 54, -128, -60, 6],      // 캐나다 남부
  [15, 32, -117, -87, 5],      // 멕시코
  [8, 17, -92, -77, 1.5],      // 중미
  [17.5, 20, -72, -65.5, 0.5], // 도미니카·푸에르토리코
  [-4, 11, -78, -67, 2.5],     // 콜롬비아
  [-18, 1, -81, -69, 3],       // 에콰도르·페루
  [-22, -10, -69, -58, 0.8],   // 볼리비아
  [-33, -3, -58, -35, 9],      // 브라질
  [-35, -30, -58, -53, 0.7],   // 우루과이
  [-55, -22, -73, -53, 5],     // 아르헨티나
  [-45, -18, -74, -67, 2],     // 칠레
  [63.3, 66.5, -24, -13.5, 0.5], // 아이슬란드
  [50, 58.5, -10, 1.8, 5],     // 영국·아일랜드
  [43, 51, -5, 8, 7],          // 프랑스·베네룩스
  [36, 44, -9.5, 3.3, 5],      // 이베리아
  [37, 47, 7, 18.5, 4],        // 이탈리아·스위스·오스트리아
  [47, 55, 6, 24, 7],          // 독일·폴란드·체코
  [55, 69, 5, 30, 4],          // 북유럽
  [54, 59.5, 21, 28, 1],       // 발트 3국
  [41, 48.5, 14, 30, 4],       // 발칸·헝가리·루마니아
  [35, 41.5, 20, 26.5, 1],     // 그리스
  [36, 42, 26, 45, 3],         // 튀르키예
  [45, 52, 22, 40, 2],         // 우크라이나
  [50, 60, 30, 60, 5],         // 러시아 서부
  [51, 57, 60, 135, 2],        // 러시아 시베리아 횡단 축
  [41, 54, 51, 87, 1.5],       // 카자흐스탄·키르기스스탄
  [43, 50, 90, 118, 0.6],      // 몽골
  [29.5, 33.3, 34.2, 36.5, 0.5], // 이스라엘·요르단
  [22, 26, 51, 57, 0.4],       // UAE·오만
  [8, 31, 70, 88, 4],          // 인도
  [6, 9.8, 79.7, 81.9, 0.5],   // 스리랑카
  [21, 26, 88, 92.5, 0.5],     // 방글라데시
  [6, 20, 98, 105, 3],         // 태국
  [9, 22, 102, 109, 1.5],      // 캄보디아·라오스·베트남
  [1, 6.7, 100, 104.5, 1.5],   // 말레이시아·싱가포르
  [-8.8, 5, 95, 116, 4],       // 인도네시아(수마트라·자바·발리)
  [5, 19, 117, 126.5, 2.5],    // 필리핀
  [22, 25.3, 120, 122, 1],     // 대만
  [34.5, 38.5, 126, 129.5, 2], // 대한민국
  [31, 45.5, 129.5, 145.5, 6], // 일본
  [-38.5, -16, 138, 153.6, 5], // 호주 동부
  [-35, -30, 114.5, 123, 1.5], // 호주 남서부
  [-38, -31, 129, 140, 1],     // 호주 남부
  [-43.5, -40.5, 144.5, 148.5, 0.4], // 태즈메이니아
  [-46.5, -34.5, 166.5, 178.5, 2],   // 뉴질랜드
  [-34.5, -22, 17, 33, 4],     // 남아프리카공화국
  [-28, -17, 14, 29, 1],       // 보츠와나·나미비아
  [-7, 4, 29, 41, 1.5],        // 케냐·우간다·탄자니아
  [4.5, 15, -17.5, 9, 1.5],    // 서아프리카(세네갈·가나·나이지리아)
  [31, 37.3, 7.5, 11.5, 0.5],  // 튀니지
];

const TOTAL_WEIGHT = REGIONS.reduce((sum, r) => sum + r[4], 0);

function randomPoint() {
  let pick = Math.random() * TOTAL_WEIGHT;
  let region = REGIONS[0];
  for (const r of REGIONS) {
    pick -= r[4];
    if (pick <= 0) { region = r; break; }
  }
  const [south, north, west, east] = region;
  // 면적 균등 분포를 위해 위도는 sin 공간에서 샘플링
  const toRad = Math.PI / 180;
  const s1 = Math.sin(south * toRad);
  const s2 = Math.sin(north * toRad);
  const lat = Math.asin(s1 + Math.random() * (s2 - s1)) / toRad;
  const lng = west + Math.random() * (east - west);
  return { lat, lng };
}

/**
 * 스트리트뷰가 존재하는 무작위 위치를 찾는다.
 * @returns {{lat:number, lng:number, panoId:string, heading:number, description:string}}
 */
export async function findRandomPanorama(onAttempt, maxAttempts = 60, isCancelled = () => false) {
  const sv = new google.maps.StreetViewService();
  for (let i = 1; i <= maxAttempts; i++) {
    if (isCancelled()) throw new Error('cancelled');
    if (authFailed) throw new Error('Google Maps API 인증 실패: API 키를 확인하세요.');
    onAttempt?.(i);
    try {
      const { data } = await sv.getPanorama(panoRequest(randomPoint(), 50000));
      const loc = data?.location;
      // 이동 링크가 없는 고립된 파노라마(실내·단독 사진 등)는 제외
      if (loc?.pano && loc.latLng && (data.links?.length ?? 0) > 0) {
        return {
          lat: loc.latLng.lat(),
          lng: loc.latLng.lng(),
          panoId: loc.pano,
          heading: Math.random() * 360,
          description: loc.description || '',
        };
      }
    } catch {
      /* ZERO_RESULTS → 재시도 */
    }
  }
  throw new Error('스트리트뷰 위치를 찾지 못했습니다. 다시 시도해주세요.');
}
