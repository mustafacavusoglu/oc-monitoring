export type Series = { key: string; label: string; color: string }

export function Legend({ series }: { series: Series[] }) {
  return <ul className="legend">
    {series.map((item) => <li key={item.key}><span className="swatch" style={{ background: item.color }} />{item.label}</li>)}
  </ul>
}
