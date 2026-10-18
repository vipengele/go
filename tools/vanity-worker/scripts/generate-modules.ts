/// <reference types="node" />
// biome-ignore lint/correctness/noNodejsModules: the go.mod walk runs in Node from CI, never in the Worker
import { readdirSync } from "node:fs";
// biome-ignore lint/correctness/noNodejsModules: the go.mod walk runs in Node from CI, never in the Worker
import { join, relative, sep } from "node:path";

const skippedDirs = new Set(["node_modules", ".git", "vendor", "testdata"]);

/** Finds every directory under root, at any depth, that holds a go.mod, relative to root with forward slashes. */
export function findGoModDirs(root: string): string[] {
  const found: string[] = [];
  const walk = (dir: string): void => {
    const entries = readdirSync(dir, { withFileTypes: true });
    if (entries.some((e) => e.isFile() && e.name === "go.mod")) {
      found.push(relative(root, dir).split(sep).join("/"));
    }
    for (const e of entries) {
      if (e.isDirectory() && !skippedDirs.has(e.name)) {
        walk(join(dir, e.name));
      }
    }
  };
  walk(root);
  return found.filter((d) => d !== "");
}
