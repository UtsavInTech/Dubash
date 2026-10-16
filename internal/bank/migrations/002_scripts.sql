-- Scripts, phase 3: every version of every uploaded or pasted script.
CREATE TABLE script_documents (
    row_id      bigserial PRIMARY KEY,
    script_id   text NOT NULL,
    version     int  NOT NULL,
    current     boolean NOT NULL DEFAULT true,
    event_id    text NOT NULL,
    name        text NOT NULL,
    text        text NOT NULL,
    status      text NOT NULL CHECK (status IN ('active', 'retired')),
    uploaded_at timestamptz NOT NULL,
    uploaded_by text NOT NULL DEFAULT '',
    UNIQUE (script_id, version)
);
CREATE UNIQUE INDEX script_documents_current ON script_documents (script_id) WHERE current;
CREATE INDEX script_documents_event ON script_documents (event_id) WHERE current;

-- A published version carries the script chunks it was compiled with.
ALTER TABLE event_context_versions ADD COLUMN script jsonb NOT NULL DEFAULT '[]';
