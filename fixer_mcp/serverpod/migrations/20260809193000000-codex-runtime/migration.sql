BEGIN;

CREATE TABLE "codex_thread" (
  "id" bigserial PRIMARY KEY,
  "threadId" text NOT NULL,
  "title" text,
  "cwd" text,
  "model" text,
  "reasoningEffort" text,
  "sandboxMode" text,
  "isActive" boolean NOT NULL DEFAULT false,
  "activeTurnId" text,
  "activeSince" timestamp without time zone,
  "lastActivityAt" timestamp without time zone,
  "lastCompletedAt" timestamp without time zone,
  "createdAt" timestamp without time zone NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX "codex_thread_thread_id_unique_idx"
  ON "codex_thread" USING btree ("threadId");

CREATE TABLE "codex_message" (
  "id" bigserial PRIMARY KEY,
  "codexThreadId" bigint NOT NULL,
  "role" text NOT NULL,
  "text" text NOT NULL,
  "turnId" text,
  "createdAt" timestamp without time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "codex_message_fk_0"
    FOREIGN KEY ("codexThreadId") REFERENCES "codex_thread" ("id") ON DELETE CASCADE
);
CREATE INDEX "codex_message_thread_created_idx"
  ON "codex_message" USING btree ("codexThreadId", "createdAt");

CREATE TABLE "codex_turn_event" (
  "id" bigserial PRIMARY KEY,
  "codexThreadId" bigint NOT NULL,
  "turnId" text NOT NULL,
  "method" text NOT NULL,
  "json" text NOT NULL,
  "createdAt" timestamp without time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "codex_turn_event_fk_0"
    FOREIGN KEY ("codexThreadId") REFERENCES "codex_thread" ("id") ON DELETE CASCADE
);
CREATE INDEX "codex_turn_event_turn_id_id_idx"
  ON "codex_turn_event" USING btree ("turnId", "id");

INSERT INTO "serverpod_migrations" ("module", "version", "timestamp")
  VALUES ('fixer_dashboard', '20260809193000000-codex-runtime', now())
  ON CONFLICT ("module")
  DO UPDATE SET "version" = '20260809193000000-codex-runtime', "timestamp" = now();

COMMIT;
