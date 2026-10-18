import { describe, expect, it, vi } from "vitest";
import worker, { createHandler } from "./index";
import { MODULE_LIST_KEY, ModuleListCache } from "./modules-source";

const BASE = "https://go.vipengele.org";
const MODULES = ["cli/core", "cli/spec", "update"];

/** A bucket binding whose get() is the given function. */
function fakeBucket(
  get: (key: string) => Promise<{ text(): Promise<string> } | null>,
): R2Bucket {
  return { get: vi.fn(get) } as unknown as R2Bucket;
}

/** A bucket binding holding body under the module list key and nothing else. */
function bucketWith(body: string): R2Bucket {
  return fakeBucket(async (key) =>
    key === MODULE_LIST_KEY ? { text: async () => body } : null,
  );
}

function getFrom(bucket: R2Bucket, path: string): Promise<Response> {
  return createHandler(new ModuleListCache()).fetch(
    new Request(`${BASE}${path}`),
    { MODULE_LIST: bucket },
  );
}

function get(path: string): Promise<Response> {
  return getFrom(bucketWith(JSON.stringify(MODULES)), path);
}

describe("worker", () => {
  it("answers a go-get request for a module with its go-import page", async () => {
    const res = await get("/cli/core?go-get=1");
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("text/html; charset=utf-8");
    expect(await res.text()).toContain(
      '<meta name="go-import" content="go.vipengele.org/cli/core git https://github.com/vipengele/go source/cli/core">',
    );
  });

  it("answers a go-get request for a sub-package with the module's page", async () => {
    const res = await get("/cli/core/internal/x?go-get=1");
    expect(res.status).toBe(200);
    expect(await res.text()).toContain(
      'content="go.vipengele.org/cli/core git https://github.com/vipengele/go source/cli/core"',
    );
  });

  it("answers a go-get request with a trailing slash", async () => {
    expect((await get("/update/?go-get=1")).status).toBe(200);
  });

  it("answers 404 for a grouping directory", async () => {
    expect((await get("/cli?go-get=1")).status).toBe(404);
  });

  it("answers 404 for an unknown module", async () => {
    expect((await get("/nope?go-get=1")).status).toBe(404);
  });

  it("answers 404 for a go-get request for the root path", async () => {
    expect((await get("/?go-get=1")).status).toBe(404);
  });

  it("redirects the root path to the module root on pkg.go.dev", async () => {
    const res = await get("/");
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toBe(
      "https://pkg.go.dev/go.vipengele.org",
    );
  });

  it("does not match a module on a partial segment", async () => {
    expect((await get("/cli/corex?go-get=1")).status).toBe(404);
  });

  it("answers 404 for a hostile path", async () => {
    expect((await get("/%3Cscript%3E?go-get=1")).status).toBe(404);
  });

  it("redirects a request without go-get to pkg.go.dev", async () => {
    const res = await get("/cli/core/x");
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toBe(
      "https://pkg.go.dev/go.vipengele.org/cli/core/x",
    );
  });

  it("drops the trailing slash from the pkg.go.dev redirect", async () => {
    const res = await get("/cli/core/");
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toBe(
      "https://pkg.go.dev/go.vipengele.org/cli/core",
    );
  });

  it("redirects when go-get is not 1", async () => {
    expect((await get("/cli/core?go-get=0")).status).toBe(302);
  });

  it("escapes HTML-significant characters of the module name", async () => {
    const handler = createHandler({ get: async () => ["a&b'c"] });
    const res = await handler.fetch(new Request(`${BASE}/a&b'c?go-get=1`), {
      MODULE_LIST: bucketWith("[]"),
    });
    expect(res.status).toBe(200);
    const body = await res.text();
    expect(body).toContain(
      'content="go.vipengele.org/a&amp;b&#39;c git https://github.com/vipengele/go source/a&amp;b&#39;c"',
    );
    expect(body).toContain("<title>go.vipengele.org/a&amp;b&#39;c</title>");
    expect(body).not.toContain("a&b'c");
  });

  it("answers from the bucket bound as MODULE_LIST", async () => {
    const res = await worker.fetch(new Request(`${BASE}/update?go-get=1`), {
      MODULE_LIST: bucketWith(JSON.stringify(MODULES)),
    });
    expect(res.status).toBe(200);
  });

  it("redirects a request without go-get without reading the bucket", async () => {
    const bucket = bucketWith(JSON.stringify(MODULES));
    expect((await getFrom(bucket, "/cli/core")).status).toBe(302);
    expect(bucket.get).not.toHaveBeenCalled();
  });
});

describe("worker without a usable module list", () => {
  it("answers 503 when the object is absent", async () => {
    const res = await getFrom(fakeBucket(async () => null), "/cli/core?go-get=1");
    expect(res.status).toBe(503);
    expect(await res.text()).toBe("Module list unavailable\n");
  });

  it("answers 503 when reading the object throws", async () => {
    const bucket = fakeBucket(async () => {
      throw new Error("R2 is down");
    });
    expect((await getFrom(bucket, "/cli/core?go-get=1")).status).toBe(503);
  });

  it("answers 503 when the object is not JSON", async () => {
    expect((await getFrom(bucketWith("not json"), "/cli/core?go-get=1")).status).toBe(503);
  });

  it("answers 503 when the schema rejects the list", async () => {
    const bucket = bucketWith(JSON.stringify(["cli", "cli/core"]));
    expect((await getFrom(bucket, "/cli/core?go-get=1")).status).toBe(503);
  });
});
