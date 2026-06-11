"use client";

interface DiffLine {
  type: "same" | "add" | "remove";
  text: string;
}

function simpleDiff(oldText: string, newText: string): DiffLine[] {
  const oldLines = oldText.split("\n");
  const newLines = newText.split("\n");
  const result: DiffLine[] = [];
  let i = 0, j = 0;

  while (i < oldLines.length || j < newLines.length) {
    if (i >= oldLines.length) {
      result.push({ type: "add", text: newLines[j] });
      j++;
    } else if (j >= newLines.length) {
      result.push({ type: "remove", text: oldLines[i] });
      i++;
    } else if (oldLines[i] === newLines[j]) {
      result.push({ type: "same", text: oldLines[i] });
      i++; j++;
    } else {
      // 简单启发: 如果下一行能匹配,则当前行是删除/添加
      const oldNextInNew = newLines.indexOf(oldLines[i], j);
      const newNextInOld = oldLines.indexOf(newLines[j], i);

      if (oldNextInNew !== -1 && (newNextInOld === -1 || oldNextInNew - j <= newNextInOld - i)) {
        // j 到 oldNextInNew-1 是新增
        for (let k = j; k < oldNextInNew; k++) {
          result.push({ type: "add", text: newLines[k] });
        }
        j = oldNextInNew;
      } else if (newNextInOld !== -1) {
        for (let k = i; k < newNextInOld; k++) {
          result.push({ type: "remove", text: oldLines[k] });
        }
        i = newNextInOld;
      } else {
        // 都不匹配,视为替换(先删后加)
        result.push({ type: "remove", text: oldLines[i] });
        result.push({ type: "add", text: newLines[j] });
        i++; j++;
      }
    }
  }

  return result;
}

export default function YamlDiff({ oldText, newText }: { oldText: string; newText: string }) {
  const lines = simpleDiff(oldText, newText);

  return (
    <div
      style={{
        fontFamily: "var(--font-mono), monospace",
        fontSize: 12,
        lineHeight: 1.6,
        overflow: "auto",
        maxHeight: "60vh",
        border: "1px solid var(--border)",
        borderRadius: 6,
      }}
    >
      {lines.map((line, idx) => {
        const bg =
          line.type === "add"
            ? "rgba(52,211,153,0.08)"
            : line.type === "remove"
            ? "rgba(248,113,113,0.08)"
            : "transparent";
        const prefix = line.type === "add" ? "+ " : line.type === "remove" ? "- " : "  ";
        const color =
          line.type === "add"
            ? "var(--success)"
            : line.type === "remove"
            ? "var(--danger)"
            : "var(--fg-mute)";

        return (
          <div
            key={idx}
            style={{
              background: bg,
              padding: "1px 8px",
              whiteSpace: "pre",
              color,
            }}
          >
            {prefix}
            {line.text}
          </div>
        );
      })}
    </div>
  );
}
