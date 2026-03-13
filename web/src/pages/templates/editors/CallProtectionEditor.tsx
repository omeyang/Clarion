import { Input, InputNumber, Space } from 'antd';

export interface CallProtectionConfig {
  asr_timeout_ms: number;
  llm_timeout_ms: number;
  tts_timeout_ms: number;
  max_consecutive_errors: number;
  max_duration_seconds: number;
  silence_timeout_ms: number;
  fallback_script: string;
}

interface Props {
  value: CallProtectionConfig | null;
  onChange: (val: CallProtectionConfig) => void;
  disabled?: boolean;
}

function getDefault(): CallProtectionConfig {
  return {
    asr_timeout_ms: 5000,
    llm_timeout_ms: 8000,
    tts_timeout_ms: 3000,
    max_consecutive_errors: 3,
    max_duration_seconds: 300,
    silence_timeout_ms: 5000,
    fallback_script: '抱歉，我没有听清楚，您能再说一遍吗？',
  };
}

const FIELDS: { key: keyof CallProtectionConfig; label: string; unit: string; isText?: boolean }[] = [
  { key: 'asr_timeout_ms', label: 'ASR 超时', unit: '毫秒' },
  { key: 'llm_timeout_ms', label: 'LLM 超时', unit: '毫秒' },
  { key: 'tts_timeout_ms', label: 'TTS 超时', unit: '毫秒' },
  { key: 'max_consecutive_errors', label: '最大连续错误次数', unit: '次' },
  { key: 'max_duration_seconds', label: '最大通话时长', unit: '秒' },
  { key: 'silence_timeout_ms', label: '静音超时', unit: '毫秒' },
  { key: 'fallback_script', label: '兜底话术', unit: '', isText: true },
];

export default function CallProtectionEditor({ value, onChange, disabled }: Props) {
  const config = value ?? getDefault();

  const update = (key: keyof CallProtectionConfig, val: number | string | null) => {
    onChange({ ...config, [key]: val ?? 0 });
  };

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      {FIELDS.map(({ key, label, unit, isText }) => (
        <div key={key}>
          <label style={{ fontWeight: 500, display: 'block', marginBottom: 4 }}>{label}</label>
          {isText ? (
            <Input.TextArea
              value={config[key] as string}
              onChange={(e) => update(key, e.target.value)}
              disabled={disabled}
              rows={3}
            />
          ) : (
            <Space>
              <InputNumber
                value={config[key] as number}
                onChange={(val) => update(key, val)}
                disabled={disabled}
                min={0}
                style={{ width: 200 }}
              />
              <span style={{ color: '#999' }}>{unit}</span>
            </Space>
          )}
        </div>
      ))}
    </Space>
  );
}
