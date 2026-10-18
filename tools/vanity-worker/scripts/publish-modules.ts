/// <reference types="node" />
import { PutObjectCommand, S3Client } from "@aws-sdk/client-s3";
// biome-ignore lint/correctness/noNodejsModules: the publisher runs in Node from CI, never in the Worker
import { dirname, resolve } from "node:path";
// biome-ignore lint/correctness/noNodejsModules: the publisher runs in Node from CI, never in the Worker
import process from "node:process";
// biome-ignore lint/correctness/noNodejsModules: the publisher runs in Node from CI, never in the Worker
import { fileURLToPath } from "node:url";
import { renderModuleList, validateModuleList } from "../src/module-list.ts";
import { findGoModDirs } from "./generate-modules.ts";

/** The object key the module list is published under in the R2 bucket. */
export const moduleListKey = "modules.json";

/** One whole-object write: the store replaces the object at key with body, or leaves it as it was. */
export interface PutObjectRequest {
  bucket: string;
  key: string;
  body: string;
  contentType: string;
}

/** The single operation the publisher needs from an object store. */
export interface ObjectStore {
  putObject(request: PutObjectRequest): Promise<void>;
}

/** What an upload to R2 needs, read from the environment. */
export interface R2Config {
  accountId: string;
  accessKeyId: string;
  secretAccessKey: string;
  bucket: string;
}

/** A Cloudflare account ID: 32 lower-case hex digits. It becomes the endpoint's host name. */
const accountIdPattern = /^[0-9a-f]{32}$/;

/**
 * Reads the R2 configuration from env. Throws naming every missing variable,
 * never a value, when any is unset or empty.
 */
export function r2ConfigFromEnv(env: Readonly<Record<string, string | undefined>>): R2Config {
  const names = ["CLOUDFLARE_ACCOUNT_ID", "R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY", "R2_BUCKET"] as const;
  const missing = names.filter((name) => !env[name]);
  if (missing.length > 0) {
    throw new Error(`${missing.join(", ")} must be set to upload the module list; pass --dry-run to upload nothing`);
  }
  const accountId = env.CLOUDFLARE_ACCOUNT_ID ?? "";
  if (!accountIdPattern.test(accountId)) {
    throw new Error(`CLOUDFLARE_ACCOUNT_ID does not match ${accountIdPattern.source}`);
  }
  return {
    accountId,
    accessKeyId: env.R2_ACCESS_KEY_ID ?? "",
    secretAccessKey: env.R2_SECRET_ACCESS_KEY ?? "",
    bucket: env.R2_BUCKET ?? "",
  };
}

/** An ObjectStore that writes to R2 through its S3-compatible API. */
export function r2Store(config: R2Config): ObjectStore {
  const client = new S3Client({
    region: "auto",
    endpoint: `https://${config.accountId}.r2.cloudflarestorage.com`,
    credentials: { accessKeyId: config.accessKeyId, secretAccessKey: config.secretAccessKey },
    // R2 does not accept every flexible checksum the SDK sends by default;
    // a checksum is computed only where an operation requires one.
    requestChecksumCalculation: "WHEN_REQUIRED",
  });
  return {
    async putObject(request) {
      await client.send(
        new PutObjectCommand({
          Bucket: request.bucket,
          Key: request.key,
          Body: request.body,
          ContentType: request.contentType,
        }),
      );
    },
  };
}

/**
 * Returns the upload of the module list for the given go.mod directories to
 * bucket. The list is validated by validateModuleList first, so a malformed
 * list throws before anything is sent.
 */
export function moduleListUpload(goModDirs: readonly string[], bucket: string): PutObjectRequest {
  return {
    bucket,
    key: moduleListKey,
    body: renderModuleList(validateModuleList(goModDirs)),
    contentType: "application/json",
  };
}

export interface RunOptions {
  argv: readonly string[];
  env: Readonly<Record<string, string | undefined>>;
  /** The directories under source/ that hold a go.mod. */
  goModDirs: () => readonly string[];
  createStore: (config: R2Config) => ObjectStore;
  log: (line: string) => void;
}

/**
 * Generates, validates and uploads the module list. With --dry-run it prints
 * the upload instead and reads no credential.
 *
 * The upload is one PutObject of the whole list, so it is idempotent and
 * atomic: an unchanged list overwrites the object with identical content, and
 * a failed upload throws and leaves the previous object in place.
 */
export async function run(options: RunOptions): Promise<void> {
  const dryRun = options.argv.includes("--dry-run");
  const unknown = options.argv.filter((arg) => arg !== "--dry-run");
  if (unknown.length > 0) {
    throw new Error(`unknown argument ${unknown.join(" ")}; usage: publish-modules [--dry-run]`);
  }
  const goModDirs = options.goModDirs();
  if (dryRun) {
    const request = moduleListUpload(goModDirs, options.env.R2_BUCKET || "<R2_BUCKET>");
    options.log(`dry run: would upload s3://${request.bucket}/${request.key} (${request.contentType}):`);
    options.log(request.body.trimEnd());
    return;
  }
  const config = r2ConfigFromEnv(options.env);
  const request = moduleListUpload(goModDirs, config.bucket);
  const store = options.createStore(config);
  try {
    await store.putObject(request);
  } catch (err) {
    const reason = err instanceof Error ? err.message : String(err);
    throw new Error(`upload of s3://${request.bucket}/${request.key} failed: ${reason}`);
  }
  options.log(`uploaded s3://${request.bucket}/${request.key}:`);
  options.log(request.body.trimEnd());
}

export interface MainOptions {
  argv: readonly string[];
  env: Readonly<Record<string, string | undefined>>;
  /** The source/ directory the go.mod files are found under. */
  sourceRoot: string;
  createStore: (config: R2Config) => ObjectStore;
  /** Ends the process with the given status. */
  exit: (code: number) => void;
}

/** The repository's source/ directory, resolved from this script's location. */
function defaultSourceRoot(): string {
  return resolve(dirname(fileURLToPath(import.meta.url)), "../../../source");
}

/**
 * Runs the publisher as the command line does: its output goes to stdout, and
 * any failure is printed to stderr and exits with status 1.
 */
export async function main(options: Partial<MainOptions> = {}): Promise<void> {
  const sourceRoot = options.sourceRoot ?? defaultSourceRoot();
  try {
    await run({
      argv: options.argv ?? process.argv.slice(2),
      env: options.env ?? process.env,
      goModDirs: () => findGoModDirs(sourceRoot),
      createStore: options.createStore ?? r2Store,
      log: (line) => console.log(line),
    });
  } catch (err) {
    console.error(err instanceof Error ? err.message : err);
    (options.exit ?? process.exit)(1);
  }
}

if (process.argv[1] !== undefined && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main();
}
