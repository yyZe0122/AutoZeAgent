ALTER TABLE plans ADD COLUMN document TEXT NOT NULL DEFAULT '{}'
    CHECK (json_valid(document));
