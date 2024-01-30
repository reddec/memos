ALTER TABLE resource
    ADD COLUMN storage_id INTEGER REFERENCES storage(id); -- NULL means use legacy logic (backward compatibility)
