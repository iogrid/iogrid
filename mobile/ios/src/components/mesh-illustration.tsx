/**
 * MeshIllustration — the onboarding hero visual.
 *
 * Replaces the emoji-house + ASCII-connector cluster (`🏠🏠🏠`, `╲ │ ╱`)
 * with a designed SVG: three provider nodes routing down to "you", drawn
 * entirely in the locked monochrome system (#684). The single permitted
 * accent (the connected-state green) marks the live route — the one
 * moment of color mirrors the connect ring the user is about to meet on
 * the home screen.
 *
 * Geometry notes: nodes sit on a 280×220 viewBox grid; edges are quadratic
 * curves that bow gently toward the center so the trio reads as a mesh,
 * not a fan. Dashed gray edges = available peers; the solid accent edge
 * = your active route. House glyphs are stroke-drawn (roof + body + door)
 * to echo "real homes" without clip-art.
 */

import Svg, { Circle, Path, Rect } from 'react-native-svg';

interface Props {
  width?: number;
  /** Grayscale strokes. */
  line: string;
  /** Node fill (elevated surface). */
  nodeFill: string;
  /** Node border. */
  nodeBorder: string;
  /** Strong text color — the "you" ring + glyph strokes. */
  ink: string;
  /** The single accent — the live route + you-node pulse. */
  accent: string;
}

function HouseGlyph({ x, y, stroke }: { x: number; y: number; stroke: string }) {
  // 28×24 stroke house: roof apex (x+14,y) → eaves, body, door.
  return (
    <>
      <Path
        d={`M${x} ${y + 10} L${x + 14} ${y} L${x + 28} ${y + 10}`}
        stroke={stroke}
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
      />
      <Path
        d={`M${x + 3.5} ${y + 9} V${y + 23} H${x + 24.5} V${y + 9}`}
        stroke={stroke}
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
      />
      <Rect
        x={x + 11}
        y={y + 14.5}
        width={6}
        height={8.5}
        rx={1.5}
        stroke={stroke}
        strokeWidth={1.6}
        fill="none"
      />
    </>
  );
}

export function MeshIllustration({
  width = 280,
  line,
  nodeFill,
  nodeBorder,
  ink,
  accent,
}: Props) {
  const height = (width / 280) * 220;
  return (
    <Svg width={width} height={height} viewBox="0 0 280 220" fill="none">
      {/* ── Edges: two available (dashed gray), one live (solid accent) ── */}
      <Path
        d="M50 78 C 60 130, 105 160, 134 178"
        stroke={line}
        strokeWidth={2}
        strokeDasharray="2 7"
        strokeLinecap="round"
        fill="none"
      />
      <Path
        d="M230 78 C 220 130, 175 160, 146 178"
        stroke={line}
        strokeWidth={2}
        strokeDasharray="2 7"
        strokeLinecap="round"
        fill="none"
      />
      <Path
        d="M140 80 L140 168"
        stroke={accent}
        strokeWidth={2.5}
        strokeLinecap="round"
        fill="none"
      />
      {/* Direction tick on the live route */}
      <Path
        d="M133 158 L140 168 L147 158"
        stroke={accent}
        strokeWidth={2.5}
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
      />

      {/* ── Provider nodes (rounded cards with stroke-drawn homes) ── */}
      {[
        { cx: 50, live: false },
        { cx: 140, live: true },
        { cx: 230, live: false },
      ].map(({ cx, live }) => (
        <Rect
          key={`node-${cx}`}
          x={cx - 27}
          y={18}
          width={54}
          height={54}
          rx={16}
          fill={nodeFill}
          stroke={live ? accent : nodeBorder}
          strokeWidth={live ? 2 : 1.5}
        />
      ))}
      <HouseGlyph x={36} y={33} stroke={ink} />
      <HouseGlyph x={126} y={33} stroke={ink} />
      <HouseGlyph x={216} y={33} stroke={ink} />

      {/* ── You ── */}
      <Circle cx={140} cy={192} r={20} fill={nodeFill} stroke={ink} strokeWidth={2} />
      {/* person glyph: head + shoulders */}
      <Circle cx={140} cy={186.5} r={4.5} stroke={ink} strokeWidth={2} fill="none" />
      <Path
        d="M131.5 200 C 133 194.5, 147 194.5, 148.5 200"
        stroke={ink}
        strokeWidth={2}
        strokeLinecap="round"
        fill="none"
      />
    </Svg>
  );
}
