-- Profile photos are gone together with object storage (R2); users keep
-- their initials. 004_avatar.sql added the column.
ALTER TABLE users DROP COLUMN IF EXISTS avatar_key;
