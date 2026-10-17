/// <reference types="node" />
import { PutObjectCommand } from "@aws-sdk/client-s3";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  type ObjectStore,
  type PutObjectRequest,
  type R2Config,
  type RunOptions,
  main,
  moduleListKey,
  moduleListUpload,
  r2ConfigFromEnv,
  r2Store,
  run,
} from "./publish-modules";

/** What the mocked S3 client was built with and sent, and the error its next send rejects with. */
const s3 = vi.hoisted(() => ({
  clientConfigs: [] as unknown[],
  sent: [] as unknown[],
  failure: undefined as Error | undefined,
}));

vi.mock("@aws-sdk/client-s3", () => ({
  S3Client: class {
    constructor(config: unknown) {
      s3.clientConfigs.push(config);
    }

    send(command: unknown): Promise<void> {
      s3.sent.push(command);
      return s3.failure === undefined ? Promise.resolve() : Promise.reject(s3.failure);
    }
  },
  PutObjectCommand: class {
    readonly input: unknown;

    constructor(input: unknown) {
      this.input = input;
    }
  },
}));

const env = {
  CLOUDFLARE_ACCOUNT_ID: "a".repeat(32),
  R2_ACCESS_KEY_ID: "test-access-key-id",
  R2_SECRET_ACCESS_KEY: "test-secret-access-key",
  R2_BUCKET: "vanity-modules",
};

/** An in-memory bucket that records every request it is sent. */
class FakeStore implements ObjectStore {
  readonly objects = new Map<string, string>();
  readonly requests: PutObjectRequest[] = [];
  failure: Error | undefined;

  async putObject(request: PutObjectRequest): Promise<void> {
    this.requests.push(request);
    if (this.failure !== undefined) {
      throw this.failure;
    }
    this.objects.set(`${request.bucket}/${request.key}`, request.body);
  }
}

function options(store: FakeStore, overrides: Partial<RunOptions> = {}) {
  const lines: string[] = [];
  const configs: R2Config[] = [];
  const opts: RunOptions = {
    argv: [],
    env,
    goModDirs: () => ["update", "cli/core", "commons"],
    createStore: (config) => {
      configs.push(config);
      return store;
    },
    log: (line) => lines.push(line),
    ...overrides,
  };
  return { opts, lines, configs };
}

describe("moduleListUpload", () => {
  it("puts the sorted JSON list under modules.json", () => {
    expect(moduleListUpload(["update", "cli/core"], "b")).toEqual({
      bucket: "b",
      key: "modules.json",
      body: '["cli/core","update"]\n',
      contentType: "application/json",
    });
  });
});

describe(r2ConfigFromEnv.name, () => {
  it("names every missing variable and no value", () => {
    expect(() => r2ConfigFromEnv({ R2_SECRET_ACCESS_KEY: "s3cr3t" })).toThrow(
      /^CLOUDFLARE_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_BUCKET must be set/,
    );
  });

  it("rejects an account ID that is not 32 hex digits", () => {
    expect(() => r2ConfigFromEnv({ ...env, CLOUDFLARE_ACCOUNT_ID: "evil.example/x" })).toThrow(
      /CLOUDFLARE_ACCOUNT_ID does not match/,
    );
  });
});

describe("run", () => {
  it("sends identical requests when an unchanged list is uploaded twice", async () => {
    const store = new FakeStore();
    const { opts, configs } = options(store);
    await run(opts);
    await run(opts);
    expect(store.requests).toHaveLength(2);
    expect(store.requests[1]).toEqual(store.requests[0]);
    expect(store.objects.get(`vanity-modules/${moduleListKey}`)).toBe('["cli/core","commons","update"]\n');
    expect(configs[0]).toEqual({
      accountId: env.CLOUDFLARE_ACCOUNT_ID,
      accessKeyId: env.R2_ACCESS_KEY_ID,
      secretAccessKey: env.R2_SECRET_ACCESS_KEY,
      bucket: env.R2_BUCKET,
    });
  });

  it("fails a rejected upload, sends nothing else and leaves the previous object", async () => {
    const previousList = `${JSON.stringify(["update"])}\n`;
    const store = new FakeStore();
    store.objects.set(`vanity-modules/${moduleListKey}`, previousList);
    store.failure = new Error("503 Service Unavailable");
    const { opts, lines } = options(store);
    await expect(run(opts)).rejects.toThrow(
      "upload of s3://vanity-modules/modules.json failed: 503 Service Unavailable",
    );
    expect(store.requests).toHaveLength(1);
    expect(store.objects.get(`vanity-modules/${moduleListKey}`)).toBe(previousList);
    expect(lines).toEqual([]);
  });

  it("prints the upload on a dry run without credentials and calls no store", async () => {
    const store = new FakeStore();
    const { opts, lines, configs } = options(store, { argv: ["--dry-run"], env: {} });
    await run(opts);
    expect(configs).toEqual([]);
    expect(store.requests).toEqual([]);
    expect(lines).toEqual([
      "dry run: would upload s3://<R2_BUCKET>/modules.json (application/json):",
      '["cli/core","commons","update"]',
    ]);
  });

  it("names the configured bucket on a dry run", async () => {
    const { opts, lines } = options(new FakeStore(), { argv: ["--dry-run"], env: { R2_BUCKET: "b" } });
    await run(opts);
    expect(lines[0]).toBe("dry run: would upload s3://b/modules.json (application/json):");
  });

  it.each([[[]], [["--dry-run"]]])("rejects a malformed list before any send (argv %j)", async (argv) => {
    const store = new FakeStore();
    const { opts, configs, lines } = options(store, { argv, goModDirs: () => ["cli", "cli/core"] });
    await expect(run(opts)).rejects.toThrow(/nested inside the module in "cli"/);
    expect(configs).toEqual([]);
    expect(store.requests).toEqual([]);
    expect(lines).toEqual([]);
  });

  it("rejects a module path outside the project path shape before any send", async () => {
    const store = new FakeStore();
    const { opts } = options(store, { goModDirs: () => ["Update"] });
    await expect(run(opts)).rejects.toThrow(/module path "Update" does not match/);
    expect(store.requests).toEqual([]);
  });

  it("fails without credentials outside a dry run and creates no store", async () => {
    const store = new FakeStore();
    const { opts, configs } = options(store, { env: {} });
    await expect(run(opts)).rejects.toThrow(/must be set to upload the module list/);
    expect(configs).toEqual([]);
    expect(store.requests).toEqual([]);
  });

  it("rejects an unknown argument", async () => {
    const { opts } = options(new FakeStore(), { argv: ["--force"] });
    await expect(run(opts)).rejects.toThrow(/unknown argument --force/);
  });
});

describe("r2Store", () => {
  const config: R2Config = {
    accountId: env.CLOUDFLARE_ACCOUNT_ID,
    accessKeyId: env.R2_ACCESS_KEY_ID,
    secretAccessKey: env.R2_SECRET_ACCESS_KEY,
    bucket: env.R2_BUCKET,
  };
  const request: PutObjectRequest = {
    bucket: "b",
    key: moduleListKey,
    body: "[]\n",
    contentType: "application/json",
  };

  beforeEach(() => {
    s3.clientConfigs.length = 0;
    s3.sent.length = 0;
    s3.failure = undefined;
  });

  it("builds an S3 client for the account's R2 endpoint", () => {
    r2Store(config);
    expect(s3.clientConfigs).toEqual([
      {
        region: "auto",
        endpoint: `https://${env.CLOUDFLARE_ACCOUNT_ID}.r2.cloudflarestorage.com`,
        credentials: { accessKeyId: env.R2_ACCESS_KEY_ID, secretAccessKey: env.R2_SECRET_ACCESS_KEY },
        requestChecksumCalculation: "WHEN_REQUIRED",
      },
    ]);
    expect(s3.sent).toEqual([]);
  });

  it("sends one PutObjectCommand carrying the request", async () => {
    await r2Store(config).putObject(request);
    expect(s3.sent).toHaveLength(1);
    expect(s3.sent[0]).toBeInstanceOf(PutObjectCommand);
    expect(s3.sent[0]).toMatchObject({
      input: { Bucket: "b", Key: moduleListKey, Body: "[]\n", ContentType: "application/json" },
    });
  });

  it("rejects with the error the send rejects with", async () => {
    s3.failure = new Error("403 Forbidden");
    await expect(r2Store(config).putObject(request)).rejects.toBe(s3.failure);
  });
});

describe("main", () => {
  let logged: string[];
  let errors: unknown[];
  let exitCodes: number[];
  const exit = (code: number) => {
    exitCodes.push(code);
  };

  beforeEach(() => {
    logged = [];
    errors = [];
    exitCodes = [];
    vi.spyOn(console, "log").mockImplementation((line: string) => {
      logged.push(line);
    });
    vi.spyOn(console, "error").mockImplementation((message: unknown) => {
      errors.push(message);
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("uploads the list of the repository's source tree and prints it", async () => {
    const store = new FakeStore();
    await main({ argv: [], env, exit, createStore: () => store });
    expect(store.requests).toHaveLength(1);
    const body = store.objects.get(`vanity-modules/${moduleListKey}`) ?? "";
    expect(JSON.parse(body)).toContain("update");
    expect(logged).toEqual([`uploaded s3://vanity-modules/${moduleListKey}:`, body.trimEnd()]);
    expect(errors).toEqual([]);
    expect(exitCodes).toEqual([]);
  });

  it("prints the failure and exits with status 1", async () => {
    const store = new FakeStore();
    store.failure = new Error("503 Service Unavailable");
    await main({ argv: [], env, exit, createStore: () => store });
    expect(errors).toEqual([`upload of s3://vanity-modules/${moduleListKey} failed: 503 Service Unavailable`]);
    expect(exitCodes).toEqual([1]);
    expect(logged).toEqual([]);
  });

  it("reads the go.mod directories from the given source root", async () => {
    const store = new FakeStore();
    await main({ argv: [], env, exit, sourceRoot: "/nonexistent-vanity-source", createStore: () => store });
    expect(String(errors[0])).toMatch(/ENOENT/);
    expect(exitCodes).toEqual([1]);
    expect(store.requests).toEqual([]);
  });

  it("reads the repository's source tree by default", async () => {
    await main({ argv: ["--dry-run"], env: {}, exit });
    expect(logged[0]).toBe(`dry run: would upload s3://<R2_BUCKET>/${moduleListKey} (application/json):`);
    expect(JSON.parse(logged[1] ?? "")).toContain("update");
    expect(exitCodes).toEqual([]);
  });
});
