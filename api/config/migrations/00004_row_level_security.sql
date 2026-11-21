-- +goose Up
-- Row-level security on every table that carries agent_id: a transaction sees
-- and writes only the rows of the agent it says it's for, set with
-- SELECT set_config('app.agent_id', <agent>, true). One that says nothing sees
-- nothing. Forced, so it holds for the tables' owner too; only a superuser
-- passes, so the platform connects as an ordinary role.
-- +goose StatementBegin
DO $$
DECLARE t text;
BEGIN
    FOR t IN SELECT c.table_name FROM information_schema.columns c
             JOIN information_schema.tables x ON x.table_schema = c.table_schema AND x.table_name = c.table_name
             WHERE c.table_schema = 'public' AND c.column_name = 'agent_id' AND x.table_type = 'BASE TABLE'
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('CREATE POLICY agent_only ON %I USING (agent_id = current_setting(''app.agent_id'', true)) '
            'WITH CHECK (agent_id = current_setting(''app.agent_id'', true))', t);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
DECLARE t text;
BEGIN
    FOR t IN SELECT tablename FROM pg_policies WHERE schemaname = 'public' AND policyname = 'agent_only'
    LOOP
        EXECUTE format('DROP POLICY agent_only ON %I', t);
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
    END LOOP;
END $$;
-- +goose StatementEnd
