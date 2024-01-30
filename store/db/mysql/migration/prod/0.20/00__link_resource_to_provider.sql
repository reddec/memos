ALTER TABLE resource
    ADD COLUMN storage_id INT DEFAULT NULL; -- NULL means use legacy logic (backward compatibility)
