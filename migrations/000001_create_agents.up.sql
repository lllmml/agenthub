-- 000001_create_agents.up.sql
-- Creates the agents table for Day 2 PostgreSQL persistence.
-- Applied manually with psql; application startup never creates tables.

CREATE TABLE agents (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT agents_name_not_blank CHECK (char_length(btrim(name)) > 0)
);
