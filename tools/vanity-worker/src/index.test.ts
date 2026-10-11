import { describe, expect, it, vi } from "vitest";
import worker from "./index";

vi.mock("./modules.generated", () => ({
  modules: ["cli/core", "cli/spec", "update", "a&b'c"],
}));

const BASE = "https://go.vipengele.org";

function get(path: string): Response {
  return worker.fetch(new Request(`${BASE}${path}`));
}

describe("worker", () => {
  it("answers a go-get request for a module with its go-import page", async () => {
    const res = get("/cli/core?go-get=1");
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("text/html; charset=utf-8");
    expect(await res.text()).toContain(
      '<meta name="go-import" content="go.vipengele.org/cli/core git https://github.com/vipengele/go source/cli/core">',
    );
  });

  it("answers a go-get request for a sub-package with the module's page", async () => {
    const res = get("/cli/core/internal/x?go-get=1");
    expect(res.status).toBe(200);
    expect(await res.text()).toContain(
      'content="go.vipengele.org/cli/core git https://github.com/vipengele/go source/cli/core"',
    );
  });

  it("answers a go-get request with a trailing slash", () => {
    expect(get("/update/?go-get=1").status).toBe(200);
  });

  it("answers 404 for a grouping directory", () => {
    expect(get("/cli?go-get=1").status).toBe(404);
  });

  it("answers 404 for an unknown module", () => {
    expect(get("/nope?go-get=1").status).toBe(404);
  });

  it("answers 404 for a go-get request for the root path", () => {
    expect(get("/?go-get=1").status).toBe(404);
  });

  it("redirects the root path to the module root on pkg.go.dev", () => {
    const res = get("/");
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toBe(
      "https://pkg.go.dev/go.vipengele.org",
    );
  });

  it("does not match a module on a partial segment", () => {
    expect(get("/cli/corex?go-get=1").status).toBe(404);
  });

  it("answers 404 for a hostile path", () => {
    expect(get("/%3Cscript%3E?go-get=1").status).toBe(404);
  });

  it("redirects a request without go-get to pkg.go.dev", () => {
    const res = get("/cli/core/x");
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toBe(
      "https://pkg.go.dev/go.vipengele.org/cli/core/x",
    );
  });

  it("drops the trailing slash from the pkg.go.dev redirect", () => {
    const res = get("/cli/core/");
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toBe(
      "https://pkg.go.dev/go.vipengele.org/cli/core",
    );
  });

  it("redirects when go-get is not 1", () => {
    expect(get("/cli/core?go-get=0").status).toBe(302);
  });

  it("escapes HTML-significant characters of the module name", async () => {
    const res = get("/a&b'c?go-get=1");
    expect(res.status).toBe(200);
    const body = await res.text();
    expect(body).toContain(
      'content="go.vipengele.org/a&amp;b&#39;c git https://github.com/vipengele/go source/a&amp;b&#39;c"',
    );
    expect(body).toContain("<title>go.vipengele.org/a&amp;b&#39;c</title>");
    expect(body).not.toContain("a&b'c");
  });
});
