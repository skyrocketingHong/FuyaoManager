ALTER TABLE audit_events ADD COLUMN details jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE audit_events ADD CONSTRAINT audit_details_object CHECK (jsonb_typeof(details) = 'object');
