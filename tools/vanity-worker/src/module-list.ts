/**
 * The shape of a project path relative to `source/`: lower-case `/`-separated
 * segments of letters, digits and `-`. It is the shape the release workflow
 * accepts after `source/` in a tag, so a path outside it names a project that
 * can never be released.
 */
export const projectPathPattern = /^[a-z0-9-]+(\/[a-z0-9-]+)*$/;

/**
 * Returns the module list for the given project paths: sorted, without
 * duplicates. Throws, naming the offending path, when a path is outside
 * `projectPathPattern` or when one path is nested inside another — the module
 * proxy resolves a nested module by its directory prefix, so the outer module
 * would swallow the inner one.
 */
export function validateModuleList(paths: readonly string[]): string[] {
  const modules = [...new Set(paths)].sort();
  for (const path of modules) {
    if (!projectPathPattern.test(path)) {
      throw new Error(
        `module path "${path}" does not match ${projectPathPattern.source}; a project directory under source/ is lower-case letters, digits and "-" separated by "/"`,
      );
    }
  }
  for (const outer of modules) {
    for (const inner of modules) {
      if (inner !== outer && inner.startsWith(`${outer}/`)) {
        throw new Error(
          `go.mod in "${inner}" is nested inside the module in "${outer}"; a module directory must not contain another module`,
        );
      }
    }
  }
  return modules;
}

/** Renders the module list as a JSON array of project paths relative to `source/`. */
export function renderModuleList(modules: readonly string[]): string {
  return `${JSON.stringify(modules)}\n`;
}
