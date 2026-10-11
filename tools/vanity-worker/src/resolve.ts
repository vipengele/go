/**
 * Returns the module that answers for a request path: the longest entry of
 * `modules` equal to the path or to a leading run of its `/`-separated
 * segments. `cli/core` answers for `cli/core` and `cli/core/internal/x`, never
 * for `cli/corex`. A grouping directory or unknown path yields `undefined`.
 *
 * `requestPath` is the URL path without its leading slash. Leading and trailing
 * slashes are ignored, and an empty segment anywhere makes the path match no
 * module.
 */
export function resolve(
  modules: readonly string[],
  requestPath: string,
): string | undefined {
  const path = requestPath.replace(/^\/+/, "").replace(/\/+$/, "");
  if (path === "" || path.split("/").some((segment) => segment === "")) {
    return undefined;
  }

  let match: string | undefined;
  for (const module of modules) {
    if (path !== module && !path.startsWith(`${module}/`)) {
      continue;
    }
    if (match === undefined || module.length > match.length) {
      match = module;
    }
  }
  return match;
}
