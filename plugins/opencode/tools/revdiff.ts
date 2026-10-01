import { tool } from "@opencode-ai/plugin";
import path from "path";

const scriptPath = path.join(
  path.dirname(new URL(import.meta.url).pathname),
  "launch-revdiff.sh",
);
const EXIT_CODE_ANNOTATIONS = 10;

const buildArgs = ({
  ref,
  only,
}: {
  ref?: string;
  only?: string[];
}): string[] => [
  ...(ref ? [ref] : []),
  ...(only?.map((f) => `--only=${f}`) ?? []),
];

export default tool({
  description:
    "Launch revdiff in a terminal overlay to review git diffs and capture annotations. " +
    "Opens an interactive diff viewer in a split pane (tmux/kitty/wezterm/ghostty/iTerm2/emacs vterm) " +
    "and returns any annotations the user wrote.",
  args: {
    ref: tool.schema
      .string()
      .optional()
      .describe(
        "Git ref to diff against (e.g. HEAD, main, a commit SHA). Omit to diff working tree.",
      ),
    only: tool.schema
      .array(tool.schema.string())
      .optional()
      .describe("Limit the diff to these file paths."),
  },
  async execute(args, context) {
    const result = await Bun.$`bash ${scriptPath} ${buildArgs(args)}`
      .cwd(context.directory)
      .quiet()
      .nothrow();
    const stdout = new TextDecoder().decode(result.stdout).trim();
    if (!isRevdiffSuccess(result.exitCode)) {
      const stderr = new TextDecoder().decode(result.stderr).trim();
      throw new Error(stderr || `revdiff exited with code ${result.exitCode}`);
    }

    return stdout || "(no annotations)";
  },
});

function isRevdiffSuccess(exitCode: number): boolean {
  return exitCode === 0 || exitCode === EXIT_CODE_ANNOTATIONS;
}
