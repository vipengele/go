/// <reference types="node" />
// biome-ignore lint/correctness/noNodejsModules: the test runs in Node, never in the Worker
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
// biome-ignore lint/correctness/noNodejsModules: the test runs in Node, never in the Worker
import { tmpdir } from "node:os";
// biome-ignore lint/correctness/noNodejsModules: the test runs in Node, never in the Worker
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { renderModuleList, validateModuleList } from "../src/module-list";
import { findGoModDirs } from "./generate-modules";

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

  it("reports nested modules so validateModuleList can reject them", () => {
    touch("cli/go.mod");
    touch("cli/core/go.mod");
    expect(() => validateModuleList(findGoModDirs(root))).toThrow(/nested/);
  });

  it("reports a directory outside the project path shape so validateModuleList can reject it", () => {
    touch("commons/go.mod");
    touch("cli/Core/go.mod");
    expect(() => validateModuleList(findGoModDirs(root))).toThrow(/"cli\/Core" does not match/);
  });

  it("renders the JSON module list of a source tree", () => {
    touch("update/go.mod");
    touch("commons/go.mod");
    touch("cli/spec/go.mod");
    touch("cli/core/go.mod");
    touch("cli/core/testdata/x/go.mod");
    expect(renderModuleList(validateModuleList(findGoModDirs(root)))).toBe(
      '["cli/core","cli/spec","commons","update"]\n',
    );
  });
});
