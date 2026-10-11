/// <reference types="node" />
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { findGoModDirs, moduleDirs, renderModules } from "./generate-modules";

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
});
