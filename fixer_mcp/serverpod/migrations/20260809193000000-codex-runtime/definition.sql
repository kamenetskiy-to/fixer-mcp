-- Codex app-server durable projection tables.
CREATE TABLE "codex_thread" (
  "id" bigserial PRIMARY KEY,
  "threadId" text NOT NULL,
  "isActive" boolean NOT NULL DEFAULT false,
  "createdAt" timestamp without time zone NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE "codex_message" (
  "id" bigserial PRIMARY KEY,
  "codexThreadId" bigint NOT NULL,
  "role" text NOT NULL,
  "text" text NOT NULL,
  "createdAt" timestamp without time zone NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE "codex_turn_event" (
  "id" bigserial PRIMARY KEY,
  "codexThreadId" bigint NOT NULL,
  "turnId" text NOT NULL,
  "method" text NOT NULL,
  "json" text NOT NULL,
  "createdAt" timestamp without time zone NOT NULL DEFAULT CURRENT_TIMESTAMP
);
