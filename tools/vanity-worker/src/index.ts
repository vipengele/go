import { ModuleListCache } from "./modules-source";
import { resolve } from "./resolve";

/** The bindings the Worker is deployed with, as wrangler.toml declares them. */
export interface Env {
  /** The bucket holding the module list object. */
  MODULE_LIST: R2Bucket;
}

const HOST = "go.vipengele.org";
const MODULE_ROOT = `${HOST}/`;
const REPOSITORY = "https://github.com/vipengele/go";
const REPOSITORY_DIR = "source/";

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

/**
 * The page the Go command reads. The module path is always the directory
 * under `source/` behind `go.vipengele.org/`, and the fourth `go-import`
 * field is that directory in the repository.
 */
function goImportPage(module: string): string {
  const importPath = escapeHtml(`${MODULE_ROOT}${module}`);
  const content = escapeHtml(
    `${MODULE_ROOT}${module} git ${REPOSITORY} ${REPOSITORY_DIR}${module}`,
  );
  return `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="go-import" content="${content}">
<title>${importPath}</title>
</head>
<body>${importPath}</body>
</html>
`;
}

/**
 * Returns the Worker's handler, answering from the module list in the
 * `MODULE_LIST` bucket through cache. The Worker owns the whole host, so the
 * request path is the URL path without its leading slash. A request without
 * `go-get=1` is a person following an import path and is redirected to the
 * package's pkg.go.dev page without reading the list; the root path redirects
 * to the module root's page.
 *
 * A go-get request is answered 503 when the list cannot be read, is absent or
 * fails validation: there is no other list to answer from, and a 404 would
 * tell the Go command a served module does not exist.
 */
export function createHandler(
  cache: Pick<ModuleListCache, "get"> = new ModuleListCache(),
) {
  return {
    async fetch(request: Request, env: Env): Promise<Response> {
      const url = new URL(request.url);
      const requestPath = url.pathname.replace(/^\/+/, "");

      if (url.searchParams.get("go-get") !== "1") {
        const path = requestPath.replace(/\/+$/, "");
        const target = new URL(
          path === ""
            ? `https://pkg.go.dev/${HOST}`
            : `https://pkg.go.dev/${MODULE_ROOT}${path}`,
        );
        return Response.redirect(target.toString(), 302);
      }

      let modules: readonly string[];
      try {
        modules = await cache.get(env.MODULE_LIST);
      } catch (err) {
        console.error(err instanceof Error ? err.message : err);
        return new Response("Module list unavailable\n", { status: 503 });
      }

      const module = resolve(modules, requestPath);
      if (module === undefined) {
        return new Response("Not Found\n", { status: 404 });
      }
      return new Response(goImportPage(module), {
        status: 200,
        headers: { "content-type": "text/html; charset=utf-8" },
      });
    },
  } satisfies ExportedHandler<Env>;
}

/** One cache per isolate, shared by every request the isolate serves. */
export default createHandler();
