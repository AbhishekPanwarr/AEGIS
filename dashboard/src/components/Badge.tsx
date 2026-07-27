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
    <span className={`badge badge-${level.toLowerCase()}`}>
      {level}
    </span>
  );
}
