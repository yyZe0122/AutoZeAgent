-- Consumer offsets must survive optional-module removal and reinstallation.
-- They therefore use the stable module ID as a logical owner without a
-- foreign key to the current module_registry row.
CREATE TABLE module_offsets_v2 (
    module_id TEXT NOT NULL,
    subscription TEXT NOT NULL,
    last_sequence INTEGER NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (module_id, subscription)
);

INSERT INTO module_offsets_v2 (module_id, subscription, last_sequence, updated_at)
SELECT module_id, subscription, last_sequence, updated_at
FROM module_offsets;

DROP TABLE module_offsets;
ALTER TABLE module_offsets_v2 RENAME TO module_offsets;
