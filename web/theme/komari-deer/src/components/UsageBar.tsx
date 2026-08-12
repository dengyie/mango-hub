import CircleChart from "./CircleChart";

interface UsageBarProps {
  value: number;
  label: string;
  compact?: boolean;
  max?: number;
}

const UsageBar = ({ value, label, compact = false, max = 100 }: UsageBarProps) => {
  const clampedValue = Math.min(Math.max(value, 0), max);

  return (
    <CircleChart
      value={clampedValue}
      label={label}
      compact={compact}
      size={compact ? 48 : 72}
    />
  );
};

export default UsageBar;
