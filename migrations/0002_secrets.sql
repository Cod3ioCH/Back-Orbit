-- +goose Up

-- secret_store holds the single key-hierarchy record for this installation.
--
-- The master passphrase is never stored, in any form. What is stored is the
-- data encryption key (DEK) wrapped with a key derived from that passphrase.
-- The indirection is what makes changing the master passphrase cheap: only
-- the wrapped DEK is rewritten, not every secret.
--
-- The KDF parameters live next to the data they protected so that raising the
-- defaults later cannot lock anyone out of an existing installation.
CREATE TABLE secret_store (
    id                INTEGER PRIMARY KEY CHECK (id = 1),
    kdf_salt          BLOB    NOT NULL,
    kdf_time_cost     INTEGER NOT NULL,
    kdf_memory_kib    INTEGER NOT NULL,
    kdf_parallelism   INTEGER NOT NULL,
    wrapped_dek       BLOB    NOT NULL,
    wrapped_dek_nonce BLOB    NOT NULL,
    key_version       INTEGER NOT NULL DEFAULT 1,
    created_at        TEXT    NOT NULL,
    updated_at        TEXT    NOT NULL
);

-- secrets holds individually encrypted values. Only ciphertext is ever
-- written here; there is deliberately no column that could hold a plaintext
-- value.
--
-- key_version records which DEK generation encrypted the row, so a rotation
-- can be verified as complete and a partially rotated store is detectable
-- rather than silently broken.
CREATE TABLE secrets (
    id          TEXT    PRIMARY KEY,
    type        TEXT    NOT NULL,
    name        TEXT    NOT NULL,
    ciphertext  BLOB    NOT NULL,
    nonce       BLOB    NOT NULL,
    key_version INTEGER NOT NULL,
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL
);

-- A secret is addressed by its kind and name, which is what callers hold on
-- to; the opaque id is what the ciphertext is cryptographically bound to.
CREATE UNIQUE INDEX idx_secrets_type_name ON secrets (type, name);

-- +goose Down
DROP TABLE secrets;
DROP TABLE secret_store;
