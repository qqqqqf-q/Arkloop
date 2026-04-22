import { readFileSync } from "node:fs"
import { render } from "@opentui/solid"
import { parseFlags, resolveConfig } from "./src/lib/config"
import { ApiClient } from "./src/api/client"
import { App } from "./src/components/App"
import { applyStartupContext, setConnected, setCurrentThreadId } from "./src/store/app"

const flags = parseFlags(process.argv.slice(2))
const config = resolveConfig(flags)
const client = new ApiClient(config)
const appVersion = readVersion()

// Resume existing thread if --resume flag provided
if (flags.resume) {
  setCurrentThreadId(flags.resume)
}

// Verify connection
try {
  const me = await client.getMe()
  applyStartupContext({
    username: me.username,
    version: appVersion,
    directory: process.cwd(),
  })
} catch (err) {
  process.stderr.write(`Failed to connect to Desktop API at ${config.host}\n`)
  process.stderr.write(err instanceof Error ? err.message : String(err))
  process.stderr.write("\n")
  setConnected(false)
  process.exit(1)
}

render(() => <App client={client} />, { exitOnCtrlC: false })

function readVersion(): string {
  for (const path of ["../desktop/package.json", "./package.json"]) {
    try {
      const raw = readFileSync(new URL(path, import.meta.url), "utf-8")
      const pkg = JSON.parse(raw) as { version?: string }
      if (typeof pkg.version === "string" && pkg.version.trim() !== "") {
        return pkg.version.trim()
      }
    } catch {
      // try next source
    }
  }
  return "0.0.0"
}
