-- +goose Up
-- Phase 4 adds three app-type adapters: laravel + wordpress (both
-- PHP-FPM under the hood) + nextjs (Node + supervisor + proxy vhost).
-- 'php' stays as the generic catch-all.
ALTER TABLE sites DROP CONSTRAINT sites_site_type_check;
ALTER TABLE sites ADD CONSTRAINT sites_site_type_check
    CHECK (site_type IN ('static', 'php', 'laravel', 'wordpress', 'nextjs', 'docker'));

-- +goose Down
ALTER TABLE sites DROP CONSTRAINT sites_site_type_check;
ALTER TABLE sites ADD CONSTRAINT sites_site_type_check
    CHECK (site_type IN ('static', 'php', 'docker'));
