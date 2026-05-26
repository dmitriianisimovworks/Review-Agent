import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";

import { getResourceByURI, RESOURCES } from "./catalog.js";
import { loadResourceText } from "./repo-context.js";

async function main(): Promise<void> {
  const server = new McpServer({
    name: "tsra-project-context",
    version: "0.1.0"
  });

  for (const resource of RESOURCES) {
    server.registerResource(
      resource.title,
      resource.uri,
      {
        title: resource.title,
        description: resource.description,
        mimeType: resource.mimeType
      },
      async () => {
        const current = getResourceByURI(resource.uri);
        if (!current) {
          throw new Error(`resource not found: ${resource.uri}`);
        }

        const text = await loadResourceText(current);
        return {
          contents: [
            {
              uri: current.uri,
              mimeType: current.mimeType,
              text
            }
          ]
        };
      }
    );
  }

  const transport = new StdioServerTransport();
  await server.connect(transport);
}

void main().catch((error) => {
  const message = error instanceof Error ? error.stack ?? error.message : String(error);
  process.stderr.write(`${message}\n`);
  process.exit(1);
});
