-- H7: pin provider/model selection ref at job create (unattended fire must not silently switch main).
ALTER TABLE jobs
    ADD COLUMN model_ref TEXT NOT NULL DEFAULT '';
