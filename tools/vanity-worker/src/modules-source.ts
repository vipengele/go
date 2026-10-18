import { validateModuleList } from "./module-list";

/** The key of the module list object in the bucket bound as `MODULE_LIST`. */
export const MODULE_LIST_KEY = "modules.json";

/**
 * How long one isolate answers from a module list it has read before reading
 * the object again. A list published to the bucket is served by every isolate
 * within this time.
 */
export const MODULE_LIST_TTL_MS = 60_000;

/** The module list object could not be read, is absent, or fails the schema. */
export class ModuleListUnavailableError extends Error {
  override name = "ModuleListUnavailableError";
}

/**
 * Reads the module list object from bucket and validates it with
 * validateModuleList. Throws ModuleListUnavailableError when the read fails,
 * the object is absent, its body is not a JSON array of strings, or the schema
 * rejects it.
 */
export async function readModuleList(bucket: R2Bucket): Promise<string[]> {
  let body: string;
  try {
    const object = await bucket.get(MODULE_LIST_KEY);
    if (object === null) {
      throw new ModuleListUnavailableError(`object ${MODULE_LIST_KEY} is absent`);
    }
    body = await object.text();
  } catch (err) {
    if (err instanceof ModuleListUnavailableError) {
      throw err;
    }
    throw new ModuleListUnavailableError(`object ${MODULE_LIST_KEY} is unreadable: ${reason(err)}`);
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch (err) {
    throw new ModuleListUnavailableError(`object ${MODULE_LIST_KEY} is not JSON: ${reason(err)}`);
  }
  if (!Array.isArray(parsed) || !parsed.every((entry) => typeof entry === "string")) {
    throw new ModuleListUnavailableError(`object ${MODULE_LIST_KEY} is not a JSON array of strings`);
  }
  try {
    return validateModuleList(parsed);
  } catch (err) {
    throw new ModuleListUnavailableError(`object ${MODULE_LIST_KEY} is rejected: ${reason(err)}`);
  }
}

function reason(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Answers the module list from one read of the object for MODULE_LIST_TTL_MS
 * after it succeeds. Concurrent callers share one read in flight. A failed read
 * is never cached and leaves no list to answer from: the next call reads
 * again, and a list past its TTL is never served.
 */
export class ModuleListCache {
  private readonly now: () => number;
  private cached: { modules: string[]; expiresAt: number } | undefined;
  private inFlight: Promise<string[]> | undefined;

  constructor(now: () => number = Date.now) {
    this.now = now;
  }

  get(bucket: R2Bucket): Promise<string[]> {
    if (this.cached !== undefined && this.now() < this.cached.expiresAt) {
      return Promise.resolve(this.cached.modules);
    }
    this.cached = undefined;
    if (this.inFlight === undefined) {
      this.inFlight = readModuleList(bucket)
        .then((modules) => {
          this.cached = { modules, expiresAt: this.now() + MODULE_LIST_TTL_MS };
          return modules;
        })
        .finally(() => {
          this.inFlight = undefined;
        });
    }
    return this.inFlight;
  }
}
