import { Flex, Typography } from "antd";

export default function PageHeader({
  title,
  subtitle,
  extra,
}: {
  title: string;
  subtitle?: string;
  extra?: React.ReactNode;
}) {
  return (
    <Flex align="flex-end" justify="space-between" wrap gap={12} style={{ marginBottom: 24 }}>
      <div>
        <Typography.Title level={3} style={{ margin: 0, fontWeight: 600 }}>
          {title}
        </Typography.Title>
        {subtitle && <Typography.Text type="secondary">{subtitle}</Typography.Text>}
      </div>
      {extra}
    </Flex>
  );
}
