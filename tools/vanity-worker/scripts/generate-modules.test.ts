/// <reference types="node" />
// biome-ignore lint/correctness/noNodejsModules: the test runs in Node, never in the Worker
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
// biome-ignore lint/correctness/noNodejsModules: the test runs in Node, never in the Worker
import { tmpdir } from "node:os";
// biome-ignore lint/correctness/noNodejsModules: the test runs in Node, never in the Worker
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderModuleList } from "../src/module-list";
import { findGoModDirs, main, moduleDirs, renderModules } from "./generate-modules";

describe("moduleDirs", () => {
  it("returns the directories sorted and without duplicates", () => {
    expect(moduleDirs(["update", "cli/core", "update", "cli/spec"])).toEqual([
      "cli/core",
      "cli/spec",
      "update",
    ]);
  });

  it("accepts sibling directories that share a name prefix", () => {
    expect(moduleDirs(["cli", "cli2"])).toEqual(["cli", "cli2"]);
  });

  it("throws naming both directories when one module is nested in another", () => {
    expect(() => moduleDirs(["cli", "cli/core"])).toThrow(/"cli\/core".*"cli"/);
  });
});

describe("renderModules", () => {
  it("renders an exported modules array", () => {
    const out = renderModules(["cli/core", "update"]);
    expect(out).toContain(
      'export const modules: readonly string[] = ["cli/core","update"];',
    );
  });
});

describe("find go.mod directories", () => {
  let root: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "vanity-worker-"));
  });

  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  function touch(relPath: string): void {
    const file = join(root, relPath);
    mkdirSync(join(file, ".."), { recursive: true });
    writeFileSync(file, "");
  }

  it("finds go.mod directories at any depth relative to the root", () => {
    touch("update/go.mod");
    touch("cli/core/go.mod");
    touch("cli/README.md");
    expect(findGoModDirs(root).sort()).toEqual(["cli/core", "update"]);
  });

  it("skips node_modules, vendor, testdata and .git", () => {
    touch("update/go.mod");
    touch("update/testdata/x/go.mod");
    touch("node_modules/y/go.mod");
    touch("vendor/z/go.mod");
    touch(".git/w/go.mod");
    expect(findGoModDirs(root)).toEqual(["update"]);
  });

  it("excludes a go.mod at the root itself", () => {
    touch("go.mod");
    touch("update/go.mod");
    expect(findGoModDirs(root)).toEqual(["update"]);
  });

  it("reports nested modules so moduleDirs can reject them", () => {
    touch("cli/go.mod");
    touch("cli/core/go.mod");
    expect(() => moduleDirs(findGoModDirs(root))).toThrow(/nested/);
  });

  it("reports a directory outside the project path shape so moduleDirs can reject it", () => {
    touch("commons/go.mod");
    touch("cli/Core/go.mod");
    expect(() => moduleDirs(findGoModDirs(root))).toThrow(/"cli\/Core" does not match/);
  });

  it("renders the JSON module list of a source tree", () => {
    touch("update/go.mod");
    touch("commons/go.mod");
    touch("cli/spec/go.mod");
    touch("cli/core/go.mod");
    touch("cli/core/testdata/x/go.mod");
    expect(renderModuleList(moduleDirs(findGoModDirs(root)))).toBe(
      '["cli/core","cli/spec","commons","update"]\n',
    );
  });
});

describe("main", () => {
  let root: string;
  let errors: unknown[];
  let exitCodes: number[];

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "vanity-generate-"));
    errors = [];
    exitCodes = [];
    vi.spyOn(console, "error").mockImplementation((message: unknown) => {
      errors.push(message);
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    rmSync(root, { recursive: true, force: true });
  });

  function paths() {
    return { sourceRoot: join(root, "source"), out: join(root, "modules.ts"), jsonOut: join(root, "modules.json") };
  }

  function touch(relPath: string): void {
    const file = join(root, "source", relPath);
    mkdirSync(join(file, ".."), { recursive: true });
    writeFileSync(file, "");
  }

  it("writes the TypeScript module and the JSON list of the source tree", () => {
    touch("update/go.mod");
    touch("cli/core/go.mod");
    main(paths(), (code) => exitCodes.push(code));
    expect(readFileSync(paths().out, "utf8")).toBe(renderModules(["cli/core", "update"]));
    expect(readFileSync(paths().jsonOut, "utf8")).toBe(renderModuleList(["cli/core", "update"]));
    expect(errors).toEqual([]);
    expect(exitCodes).toEqual([]);
  });

  it("prints why an invalid tree is rejected, exits with status 1 and writes nothing", () => {
    touch("cli/go.mod");
    touch("cli/core/go.mod");
    main(paths(), (code) => exitCodes.push(code));
    expect(errors).toHaveLength(1);
    expect(String(errors[0])).toMatch(/nested/);
    expect(exitCodes).toEqual([1]);
    expect(existsSync(paths().out)).toBe(false);
    expect(existsSync(paths().jsonOut)).toBe(false);
  });
});
