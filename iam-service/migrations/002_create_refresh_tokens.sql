CREATE TABLE IF NOT EXISTS auth.refresh_tokens (
    id          BIGSERIAL    PRIMARY KEY,
    user_id     VARCHAR(40)  NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    token_hash  VARCHAR(64)  NOT NULL,
    expires_at  TIMESTAMPTZ  NOT NULL,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    user_agent  TEXT,
    ip_address  INET,
    CONSTRAINT chk_token_hash_len CHECK (char_length(token_hash) = 64)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_refresh_tokens_hash       ON auth.refresh_tokens (token_hash);
CREATE INDEX        IF NOT EXISTS idx_refresh_tokens_user_id    ON auth.refresh_tokens (user_id);
CREATE INDEX        IF NOT EXISTS idx_refresh_tokens_expires_at ON auth.refresh_tokens (expires_at);
