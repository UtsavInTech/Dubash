-- Dictation Bank, phase 2. Applied once by internal/bank (schema_migrations).

CREATE TABLE organizations (
    id         text PRIMARY KEY,
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO organizations (id, name) VALUES ('default', 'Default') ON CONFLICT DO NOTHING;

-- Every version of every entry; "current" marks the latest.
CREATE TABLE vocabulary_entries (
    row_id        bigserial PRIMARY KEY,
    entry_id      text NOT NULL,
    version       int  NOT NULL,
    current       boolean NOT NULL DEFAULT true,
    scope         text NOT NULL CHECK (scope IN ('org', 'speaker', 'event')),
    scope_id      text NOT NULL,
    term          text NOT NULL,
    lang          text NOT NULL DEFAULT '',
    aliases       jsonb NOT NULL DEFAULT '[]',
    misheard      jsonb NOT NULL DEFAULT '[]',
    pronunciation text NOT NULL DEFAULT '',
    meaning       text NOT NULL DEFAULT '',
    keep          boolean NOT NULL DEFAULT false,
    ambiguous     boolean NOT NULL DEFAULT false,
    status        text NOT NULL CHECK (status IN ('suggested', 'approved', 'rejected', 'retired')),
    source        text NOT NULL DEFAULT '',
    note          text NOT NULL DEFAULT '',
    created_by    text NOT NULL DEFAULT '',
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (entry_id, version)
);
CREATE UNIQUE INDEX vocabulary_entries_current ON vocabulary_entries (entry_id) WHERE current;
CREATE INDEX vocabulary_entries_scope ON vocabulary_entries (scope, scope_id) WHERE current;

-- Approved translations of one entry version.
CREATE TABLE vocabulary_translations (
    row_id bigint NOT NULL REFERENCES vocabulary_entries (row_id) ON DELETE CASCADE,
    lang   text NOT NULL,
    text   text NOT NULL,
    PRIMARY KEY (row_id, lang)
);

-- Immutable snapshots of what applies to an event.
CREATE TABLE event_context_versions (
    id         bigserial PRIMARY KEY,
    event_id   text NOT NULL,
    number     int  NOT NULL,
    created_at timestamptz NOT NULL,
    created_by text NOT NULL DEFAULT '',
    note       text NOT NULL DEFAULT '',
    speaker    text NOT NULL DEFAULT '',
    hash       text NOT NULL,
    entries    jsonb NOT NULL,
    UNIQUE (event_id, number)
);

CREATE TABLE event_context_pins (
    event_id   text PRIMARY KEY,
    version_id bigint NOT NULL REFERENCES event_context_versions (id),
    pinned_at  timestamptz NOT NULL DEFAULT now(),
    pinned_by  text NOT NULL DEFAULT ''
);

CREATE TABLE transcript_corrections (
    id         bigserial PRIMARY KEY,
    event_id   text NOT NULL,
    segment_id int  NOT NULL,
    rev        int  NOT NULL,
    raw        text NOT NULL,
    corrected  text NOT NULL,
    lang       text NOT NULL DEFAULT '',
    at         timestamptz NOT NULL,
    by         text NOT NULL DEFAULT '',
    suggested  jsonb NOT NULL DEFAULT '[]'
);
CREATE INDEX transcript_corrections_event ON transcript_corrections (event_id, segment_id);

CREATE TABLE audit_log (
    id     bigserial PRIMARY KEY,
    at     timestamptz NOT NULL DEFAULT now(),
    actor  text NOT NULL,
    action text NOT NULL,
    object text NOT NULL,
    detail text NOT NULL DEFAULT ''
);
