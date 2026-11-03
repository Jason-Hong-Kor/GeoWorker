# 🌏 GeoWorker

Google Street View 기반 위치 맞히기 게임. Go(Echo + GORM, 정적 프론트엔드 embed) + PostgreSQL 모놀리식 구성.

## 실행

1. `.env` 의 `GOOGLE_MAPS_API_KEY=` 에 키 입력
   - Google Cloud Console 에서 **Maps JavaScript API** 활성화 (필수, Street View 포함)
   - **Geocoding API** 활성화 (권장, 위치 주소 저장용. 없으면 파노라마 설명/좌표로 대체)
2. `docker compose up -d --build`
3. http://localhost:8080

## 구성

| 경로 | 설명 |
|---|---|
| `main.go` | 설정, DB 연결·마이그레이션, Echo 서버·미들웨어, 정적 파일(embed) |
| `handlers.go` | REST API (Echo 핸들러, 에러 핸들러) |
| `store.go` | PostgreSQL 접근 (GORM 모델·쿼리) |
| `score.go` | 거리/시간 감점 곡선, 스코어, Haversine 거리 |
| `schema.sql` | `locations`, `game_records` 테이블 (기동 시 자동 적용) |
| `web/` | 프론트엔드 (Vanilla JS, 빌드 불필요) |

## 게임 규칙

- 출제: 랜덤(스트리트뷰 커버리지 지역에서 자동 탐색 후 `locations` 에 `random` 으로 기록) : 관리자 지정 = 8 : 2 (`ADMIN_LOCATION_RATIO`). 관리자 위치가 없으면 항상 랜덤.
- 소요 시간은 서버 기준(게임 생성 ~ Guess 제출)으로 계산.
- 스코어 = 100 − 거리 감점 − 시간 감점 (각 최대 50점, 0.1점 단위)
  - 감점 곡선 `p(x) = s·k·ln(1 + (x − free)/k)`: 시작 기울기 `s` 가 "10단위당 0.1점", `k` 는 최대값에서 정확히 50점이 되도록 자동 계산
  - 거리: 10m 이내 0점, 20,000km 이상 50점 (예: 1km 5.4, 100km 25.2, 1,000km 36.0)
  - 시간: 30초 이내 0점, 4시간 이상 50점 (예: 1분 0.3, 10분 5.2, 1시간 22.9)
- 결과 화면에서 👍(+1) / 👎(−1) 피드백 → `locations.feedback` 누적 (게임당 1회)
- 게임 종료 버튼으로 나간 미완료 게임은 기록에서 삭제됨

## API

| Method | Path | 설명 |
|---|---|---|
| GET | `/api/config` | Maps API 키, 출제 비율 |
| GET | `/api/games/draw` | 출제 방식 결정 (`random` / `admin` + location) |
| POST | `/api/games` | 게임 시작 `{locationId}` 또는 `{location:{lat,lng,panoId,heading,address}}` |
| POST | `/api/games/{id}/guess` | `{lat,lng}` → 거리·시간·스코어 |
| POST | `/api/games/{id}/feedback` | `{value: 1 \| -1}` |
| DELETE | `/api/games/{id}` | 미완료 게임 폐기 |
| GET | `/api/records` | 기록 + 통계 |
| GET/POST | `/api/admin/locations` | 위치 목록(`?source=admin\|random`) / 관리자 위치 추가 |
| DELETE | `/api/admin/locations/{id}` | 위치 삭제 (기록은 유지) |
