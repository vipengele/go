import { describe, expect, it, vi } from "vitest";
import {
  MODULE_LIST_KEY,
  MODULE_LIST_TTL_MS,
  ModuleListCache,
  ModuleListUnavailableError,
  readModuleList,
} from "./modules-source";

/** A bucket binding whose get() answers the module list key with each body in turn. */
function bucketWith(...bodies: string[]): R2Bucket & { get: ReturnType<typeof vi.fn> } {
  let next = 0;
  const get = vi.fn(async (key: string) => {
    if (key !== MODULE_LIST_KEY) {
      return null;
    }
    const body = bodies[Math.min(next++, bodies.length - 1)] ?? "";
    return { text: async () => body };
  });
  return { get } as unknown as R2Bucket & { get: ReturnType<typeof vi.fn> };
}

function failingBucket(): R2Bucket {
  return {
    get: vi.fn(async () => {
      throw new Error("R2 is down");
    }),
  } as unknown as R2Bucket;
}

describe("readModuleList", () => {
  it("reads the modules.json object", () => {
    expect(MODULE_LIST_KEY).toBe("modules.json");
  });

  it("returns the validated list", async () => {
    await expect(readModuleList(bucketWith('["update","cli/core"]'))).resolves.toEqual([
      "cli/core",
      "update",
    ]);
  });

  it("throws when the object is absent", async () => {
    const bucket = { get: vi.fn(async () => null) } as unknown as R2Bucket;
    await expect(readModuleList(bucket)).rejects.toThrow(
      new ModuleListUnavailableError("object modules.json is absent"),
    );
  });

  it("throws when reading the object throws", async () => {
    await expect(readModuleList(failingBucket())).rejects.toThrow(/unreadable: R2 is down/);
  });

  it("throws when reading the body throws", async () => {
    const bucket = {
      get: vi.fn(async () => ({
        text: async () => {
          throw new Error("stream reset");
        },
      })),
    } as unknown as R2Bucket;
    await expect(readModuleList(bucket)).rejects.toThrow(/unreadable: stream reset/);
  });

  it("throws when the object is not JSON", async () => {
    await expect(readModuleList(bucketWith("{"))).rejects.toThrow(/is not JSON/);
  });

  it.each(['{"modules":[]}', '["cli/core",1]', "null"])(
    "throws when the object %s is not a JSON array of strings",
    async (body) => {
      await expect(readModuleList(bucketWith(body))).rejects.toThrow(
        /is not a JSON array of strings/,
      );
    },
  );

  it("throws when the schema rejects the list", async () => {
    const rejected = readModuleList(bucketWith('["cli","cli/core"]'));
    await expect(rejected).rejects.toBeInstanceOf(ModuleListUnavailableError);
    await expect(readModuleList(bucketWith('["Cli"]'))).rejects.toThrow(/is rejected/);
  });
});

describe("ModuleListCache", () => {
  it("keeps a list for 60 seconds", () => {
    expect(MODULE_LIST_TTL_MS).toBe(60_000);
  });

  it("answers from the cached list within the TTL without reading the object again", async () => {
    let now = 1_000;
    const cache = new ModuleListCache(() => now);
    const bucket = bucketWith('["cli/core"]', '["update"]');

    await expect(cache.get(bucket)).resolves.toEqual(["cli/core"]);
    now += MODULE_LIST_TTL_MS - 1;
    await expect(cache.get(bucket)).resolves.toEqual(["cli/core"]);
    expect(bucket.get).toHaveBeenCalledTimes(1);
  });

  it("reads the object again once the TTL has passed", async () => {
    let now = 1_000;
    const cache = new ModuleListCache(() => now);
    const bucket = bucketWith('["cli/core"]', '["update"]');

    await cache.get(bucket);
    now += MODULE_LIST_TTL_MS;
    await expect(cache.get(bucket)).resolves.toEqual(["update"]);
    expect(bucket.get).toHaveBeenCalledTimes(2);
  });

  it("serves no expired list when the read after it fails", async () => {
    let now = 1_000;
    const cache = new ModuleListCache(() => now);
    await cache.get(bucketWith('["cli/core"]'));

    now += MODULE_LIST_TTL_MS;
    await expect(cache.get(failingBucket())).rejects.toBeInstanceOf(ModuleListUnavailableError);
  });

  it("reads the object again on the call after a failed read", async () => {
    const cache = new ModuleListCache(() => 0);
    await expect(cache.get(failingBucket())).rejects.toBeInstanceOf(ModuleListUnavailableError);
    await expect(cache.get(bucketWith('["cli/core"]'))).resolves.toEqual(["cli/core"]);
  });

  it("shares one read between concurrent calls", async () => {
    const cache = new ModuleListCache(() => 0);
    const bucket = bucketWith('["cli/core"]');

    const [a, b] = await Promise.all([cache.get(bucket), cache.get(bucket)]);
    expect(a).toEqual(["cli/core"]);
    expect(b).toEqual(["cli/core"]);
    expect(bucket.get).toHaveBeenCalledTimes(1);
  });
});
