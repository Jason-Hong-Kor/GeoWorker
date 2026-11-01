-- 출제 위치 (랜덤 자동 생성 / 관리자 지정)
CREATE TABLE IF NOT EXISTS locations (
    id          BIGSERIAL PRIMARY KEY,
    lat         DOUBLE PRECISION NOT NULL,
    lng         DOUBLE PRECISION NOT NULL,
    pano_id     TEXT             NOT NULL DEFAULT '',
    heading     DOUBLE PRECISION NOT NULL DEFAULT 0,
    address     TEXT             NOT NULL DEFAULT '',
    feedback    INTEGER          NOT NULL DEFAULT 0,          -- thumb up +1 / down -1 누적
    source      TEXT             NOT NULL CHECK (source IN ('random', 'admin')),
    created_at  TIMESTAMPTZ      NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_locations_source ON locations (source);

-- 사용자 게임 기록
CREATE TABLE IF NOT EXISTS game_records (
    id           BIGSERIAL PRIMARY KEY,
    location_id  BIGINT REFERENCES locations (id) ON DELETE SET NULL,
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at  TIMESTAMPTZ,
    guess_lat    DOUBLE PRECISION,
    guess_lng    DOUBLE PRECISION,
    distance_m   DOUBLE PRECISION,
    duration_s   DOUBLE PRECISION,
    score        DOUBLE PRECISION,
    feedback     SMALLINT CHECK (feedback IN (-1, 1))
);

CREATE INDEX IF NOT EXISTS idx_game_records_finished ON game_records (finished_at DESC);
CREATE INDEX IF NOT EXISTS idx_game_records_location ON game_records (location_id);
