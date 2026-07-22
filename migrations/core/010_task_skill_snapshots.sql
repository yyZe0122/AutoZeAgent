CREATE TABLE task_skill_snapshots (
    task_id TEXT PRIMARY KEY REFERENCES tasks(task_id),
    skill_ids TEXT NOT NULL CHECK (json_valid(skill_ids) AND json_type(skill_ids) = 'array'),
    instructions TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    created_at TEXT NOT NULL
);

INSERT INTO task_skill_snapshots (task_id, skill_ids, instructions, content_hash, created_at)
SELECT task_id, '[]', '', 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', created_at
FROM tasks;

CREATE TRIGGER task_skill_snapshots_reject_update
BEFORE UPDATE ON task_skill_snapshots
BEGIN
    SELECT RAISE(ABORT, 'task skill snapshots are immutable');
END;

CREATE TRIGGER task_skill_snapshots_reject_delete
BEFORE DELETE ON task_skill_snapshots
BEGIN
    SELECT RAISE(ABORT, 'task skill snapshots are immutable');
END;