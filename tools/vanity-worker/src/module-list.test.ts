import { describe, expect, it } from "vitest";
import { renderModuleList, validateModuleList } from "./module-list";

describe("validateModuleList", () => {
  it("returns the paths sorted and without duplicates", () => {
    expect(validateModuleList(["update", "cli/core", "update", "commons"])).toEqual([
      "cli/core",
      "commons",
      "update",
    ]);
  });

  it("accepts sibling paths that share a name prefix", () => {
    expect(validateModuleList(["cli", "cli2"])).toEqual(["cli", "cli2"]);
  });

  it("throws naming both paths when one module is nested in another", () => {
    expect(() => validateModuleList(["cli", "cli/core"])).toThrow(/"cli\/core".*"cli"/);
  });

  it.each([
    "Cli/core",
    "cli_core",
    "cli/core/",
    "/cli",
    "cli//core",
    "cli/../core",
    "cli.core",
    "",
  ])("throws naming %j when it is outside the project path shape", (path) => {
    expect(() => validateModuleList(["commons", path])).toThrow(
      `module path ${JSON.stringify(path)} does not match`,
    );
  });
});

describe("renderModuleList", () => {
  it("renders a JSON array of paths", () => {
    expect(renderModuleList(["cli/spec", "commons"])).toBe('["cli/spec","commons"]\n');
  });
});
