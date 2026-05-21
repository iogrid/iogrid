import { drizzle } from "drizzle-orm/postgres-js";
import postgres from "postgres";
import * as schema from "./schema";

// Drizzle client used by @auth/drizzle-adapter. Reads DATABASE_URL at
// request time (NOT build time — Next.js production builds run without
// the env, which is fine since this module is only imported from the
// Node-only auth surface).

const connectionString = process.env.DATABASE_URL;

const queryClient = connectionString
  ? postgres(connectionString, {
      ssl: process.env.PGSSLMODE === "disable" ? false : "prefer",
      max: 5,
    })
  : null;

export const db = queryClient
  ? drizzle(queryClient, { schema })
  : (null as unknown as ReturnType<typeof drizzle>);
