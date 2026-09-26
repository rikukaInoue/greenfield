DROP INDEX idx_photos_status_created ON photos;
ALTER TABLE photos
    DROP COLUMN object_key,
    DROP COLUMN content_type,
    DROP COLUMN size_bytes,
    DROP COLUMN status;
