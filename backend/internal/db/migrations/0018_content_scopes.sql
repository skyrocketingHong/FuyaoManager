CREATE TABLE content_scopes (
    id uuid PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('site', 'platform')),
    site_id uuid UNIQUE REFERENCES sites(id) ON DELETE CASCADE,
    CHECK ((kind = 'site' AND site_id IS NOT NULL AND id = site_id)
        OR (kind = 'platform' AND site_id IS NULL AND id = 'd6a785b1-64db-50a8-9ca9-761c4185018c'))
);
INSERT INTO content_scopes(id,kind,site_id) SELECT id,'site',id FROM sites;
INSERT INTO content_scopes(id,kind) VALUES ('d6a785b1-64db-50a8-9ca9-761c4185018c','platform');
CREATE FUNCTION create_site_content_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO content_scopes(id,kind,site_id) VALUES (NEW.id,'site',NEW.id);
    RETURN NEW;
END
$$;
CREATE TRIGGER sites_content_scope AFTER INSERT ON sites
    FOR EACH ROW EXECUTE FUNCTION create_site_content_scope();
ALTER TABLE site_media_tasks DROP CONSTRAINT site_media_tasks_site_id_fkey;
ALTER TABLE site_media_tasks ADD CONSTRAINT site_media_tasks_scope_id_fkey
    FOREIGN KEY (site_id) REFERENCES content_scopes(id) ON DELETE CASCADE;
ALTER TABLE site_media_tasks ADD COLUMN theme_id uuid REFERENCES themes(id);
UPDATE site_media_tasks t SET theme_id=s.theme_id
    FROM site_theme_selections s WHERE s.site_id=t.site_id;
