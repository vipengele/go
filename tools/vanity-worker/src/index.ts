import { modules } from "./modules.generated";
import { resolve } from "./resolve";

const PREFIX = "/go/";
const MODULE_ROOT = "vipengele.org/go/";
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
 * under `source/` behind `vipengele.org/go/`, and the fourth `go-import`
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

export default {
  fetch(request: Request): Response {
    const url = new URL(request.url);
    if (!url.pathname.startsWith(PREFIX)) {
      return new Response("Not Found\n", { status: 404 });
    }
    const requestPath = url.pathname.slice(PREFIX.length);

    if (url.searchParams.get("go-get") !== "1") {
      const target = new URL(
        `https://pkg.go.dev/${MODULE_ROOT}${requestPath.replace(/\/+$/, "")}`,
      );
      return Response.redirect(target.toString(), 302);
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
} satisfies ExportedHandler;
