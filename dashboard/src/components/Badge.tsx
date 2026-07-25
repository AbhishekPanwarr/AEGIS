interface BadgeProps {
  level: string;
}

const colors: Record<string, string> = {
  OBSERVE: '#22c55e',
  THROTTLE: '#eab308',
  QUARANTINE: '#f97316',
  HALT: '#ef4444',
};

export function Badge({ level }: BadgeProps) {
  const color = colors[level] || '#6b7280';
  return (
    <span style={{
      display: 'inline-block',
      padding: '2px 10px',
      borderRadius: '12px',
      fontSize: '12px',
      fontWeight: 600,
      color: '#fff',
      background: color,
    }}>
      {level}
    </span>
  );
}
