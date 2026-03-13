import { Checkbox, Input, Space, Switch } from 'antd';

export interface NotificationConfig {
  enabled: boolean;
  channels: string[];
  trigger_grades: string[];
  template: string;
}

interface Props {
  value: NotificationConfig | null;
  onChange: (val: NotificationConfig) => void;
  disabled?: boolean;
}

const CHANNEL_OPTIONS = [
  { label: '企业微信', value: 'wechat_work' },
  { label: '短信', value: 'sms' },
  { label: '邮件', value: 'email' },
];

const GRADE_OPTIONS = [
  { label: 'A', value: 'A' },
  { label: 'B', value: 'B' },
  { label: 'C', value: 'C' },
  { label: 'D', value: 'D' },
  { label: 'X', value: 'X' },
];

function getDefault(): NotificationConfig {
  return { enabled: false, channels: [], trigger_grades: [], template: '' };
}

export default function NotificationConfigEditor({ value, onChange, disabled }: Props) {
  const config = value ?? getDefault();

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <div>
        <label style={{ fontWeight: 500, marginRight: 12 }}>启用通知</label>
        <Switch
          checked={config.enabled}
          onChange={(checked) => onChange({ ...config, enabled: checked })}
          disabled={disabled}
        />
      </div>

      <div>
        <label style={{ fontWeight: 500, display: 'block', marginBottom: 4 }}>通知渠道</label>
        <Checkbox.Group
          options={CHANNEL_OPTIONS}
          value={config.channels}
          onChange={(vals) => onChange({ ...config, channels: vals as string[] })}
          disabled={disabled}
        />
      </div>

      <div>
        <label style={{ fontWeight: 500, display: 'block', marginBottom: 4 }}>触发等级</label>
        <Checkbox.Group
          options={GRADE_OPTIONS}
          value={config.trigger_grades}
          onChange={(vals) => onChange({ ...config, trigger_grades: vals as string[] })}
          disabled={disabled}
        />
      </div>

      <div>
        <label style={{ fontWeight: 500, display: 'block', marginBottom: 4 }}>通知模板</label>
        <Input.TextArea
          value={config.template}
          onChange={(e) => onChange({ ...config, template: e.target.value })}
          disabled={disabled}
          rows={5}
          placeholder={'【AI外呼通知】\n客户：{{name}}\n等级：{{grade}}\n摘要：{{summary}}'}
        />
      </div>
    </Space>
  );
}
