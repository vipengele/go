import { describe, expect, it } from "vitest";
import { resolve } from "./resolve";

const modules = ["cli/core", "cli/spec", "update"];

describe("resolve", () => {
  it("resolves a path equal to a module", () => {
    expect(resolve(modules, "cli/core")).toBe("cli/core");
    expect(resolve(modules, "update")).toBe("update");
  });

  it("resolves a sub-package path to its module", () => {
    expect(resolve(modules, "cli/core/internal/x")).toBe("cli/core");
  });

  it("ignores leading and trailing slashes", () => {
    expect(resolve(modules, "cli/core/")).toBe("cli/core");
    expect(resolve(modules, "/cli/core/x//")).toBe("cli/core");
  });

  it("yields undefined for a grouping directory", () => {
    expect(resolve(modules, "cli")).toBeUndefined();
  });

  it("yields undefined for an unknown path", () => {
    expect(resolve(modules, "nope")).toBeUndefined();
    expect(resolve(modules, "")).toBeUndefined();
  });

  it("yields undefined when a segment is empty", () => {
    expect(resolve(modules, "cli//core")).toBeUndefined();
  });

  it("matches only on segment boundaries", () => {
    expect(resolve(modules, "cli/corex")).toBeUndefined();
    expect(resolve(modules, "cli/corex/y")).toBeUndefined();
  });

  it("prefers the longest module when modules overlap by prefix", () => {
    const overlapping = ["cli", "cli/core"];
    expect(resolve(overlapping, "cli/core/x")).toBe("cli/core");
    expect(resolve(overlapping, "cli/other")).toBe("cli");
    expect(resolve([...overlapping].reverse(), "cli/core/x")).toBe("cli/core");
  });
});
