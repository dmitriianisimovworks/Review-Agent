import fs from "node:fs/promises";
import path from "node:path";

import { REPO_ROOT, ResourceDefinition } from "./catalog.js";

export async function loadResourceText(resource: ResourceDefinition): Promise<string> {
  if (resource.kind === "virtual") {
    return resource.virtualText ?? "";
  }

  if (!resource.relativePath) {
    throw new Error(`resource ${resource.uri} is missing relativePath`);
  }

  const resolvedPath = resolveAllowedPath(resource.relativePath);
  return fs.readFile(resolvedPath, "utf8");
}

function resolveAllowedPath(relativePath: string): string {
  const resolved = path.resolve(REPO_ROOT, relativePath);
  const normalizedRoot = path.resolve(REPO_ROOT) + path.sep;
  const normalizedResolved = path.resolve(resolved);

  if (!normalizedResolved.startsWith(normalizedRoot) && normalizedResolved !== path.resolve(REPO_ROOT)) {
    throw new Error(`path escape detected for ${relativePath}`);
  }

  return normalizedResolved;
}
