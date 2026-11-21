import { portraits, type Character } from "./portraits";

// A character's pixel portrait in a circle. size is in pixels; a multiple of 12 keeps
// every pixel sharp. It never shrinks: in a flex row beside long text it otherwise would.
export function Portrait({ who, size = 48 }: { who: Character; size?: number }) {
  const { name, map, palette } = portraits[who];
  const colour = palette as Record<string, string>;
  return (
    <svg width={size} height={size} viewBox="0 0 12 12" role="img" aria-label={name} shapeRendering="crispEdges" style={{ flexShrink: 0 }}>
      <clipPath id={`portrait-${who}`}>
        <circle cx="6" cy="6" r="6" />
      </clipPath>
      <g clipPath={`url(#portrait-${who})`}>
        {map.flatMap((row, y) =>
          [...row].map((ch, x) => <rect key={`${x}-${y}`} x={x} y={y} width="1" height="1" fill={colour[ch] ?? colour["."]} />),
        )}
      </g>
    </svg>
  );
}
