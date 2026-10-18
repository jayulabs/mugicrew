#!/usr/bin/env node
// Hands off to the platform's Go binary. No postinstall: pnpm 10 and
// --ignore-scripts skip scripts, so the bin must work as published.
const pkg = `@jayulabs/mugicrew-${process.platform}-${process.arch}`;
let bin;
try {
  bin = require.resolve(`${pkg}/bin/mugicrew`);
} catch {
  console.error(
    `mugicrew: unsupported platform or missing optional dependency ${pkg}.\n` +
      `Supported: linux-x64, linux-arm64, darwin-arm64, darwin-x64. ` +
      `Reinstall without --omit=optional.`,
  );
  process.exit(1);
}
const args = [bin, ...process.argv.slice(2)];
// execve replaces Node: one Go process, same pid, signals and exit code intact.
if (process.execve) process.execve(bin, args, process.env);

// Fallback (Node < 22.15): spawn, forward signals, mirror the child's exit.
const child = require("node:child_process").spawn(bin, args.slice(1), {
  stdio: "inherit",
  argv0: bin,
});
for (const sig of ["SIGINT", "SIGTERM", "SIGHUP"]) {
  process.on(sig, () => child.kill(sig));
}
child.on("error", (e) => {
  console.error(`mugicrew: ${e.message}`);
  process.exit(1);
});
child.on("exit", (code, signal) => {
  if (signal) {
    process.removeAllListeners(signal);
    process.kill(process.pid, signal);
  } else process.exit(code ?? 1);
});
