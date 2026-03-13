import { Card, Input, Space, Button } from 'antd';
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons';
import { useState } from 'react';

export type PromptTemplates = Record<string, string>;

interface Props {
  value: PromptTemplates | null;
  onChange: (val: PromptTemplates) => void;
  disabled?: boolean;
}

const KNOWN_KEYS = [
  { key: 'system', label: '系统提示词 (system)' },
  { key: 'Opening', label: '开场 (Opening)' },
  { key: 'Qualification', label: '资质判断 (Qualification)' },
  { key: 'InformationGathering', label: '信息收集 (InformationGathering)' },
  { key: 'ObjectionHandling', label: '异议处理 (ObjectionHandling)' },
  { key: 'NextAction', label: '下一步 (NextAction)' },
  { key: 'Closing', label: '结束 (Closing)' },
];

export default function PromptTemplatesEditor({ value, onChange, disabled }: Props) {
  const config = value ?? {};
  const [newKey, setNewKey] = useState('');

  // Collect all keys: known keys first, then any extra keys from config
  const knownKeySet = new Set(KNOWN_KEYS.map((k) => k.key));
  const extraKeys = Object.keys(config).filter((k) => !knownKeySet.has(k));
  const allEntries = [
    ...KNOWN_KEYS.map((k) => ({ key: k.key, label: k.label, removable: false })),
    ...extraKeys.map((k) => ({ key: k, label: k, removable: true })),
  ];

  const updatePrompt = (key: string, text: string) => {
    onChange({ ...config, [key]: text });
  };

  const removeKey = (key: string) => {
    const next = { ...config };
    delete next[key];
    onChange(next);
  };

  const addKey = () => {
    const trimmed = newKey.trim();
    if (trimmed && !(trimmed in config)) {
      onChange({ ...config, [trimmed]: '' });
      setNewKey('');
    }
  };

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="small">
      {allEntries.map(({ key, label, removable }) => (
        <Card
          key={key}
          size="small"
          title={label}
          extra={
            !disabled && removable ? (
              <Button type="link" danger icon={<DeleteOutlined />} onClick={() => removeKey(key)} size="small" />
            ) : undefined
          }
        >
          <Input.TextArea
            value={config[key] ?? ''}
            onChange={(e) => updatePrompt(key, e.target.value)}
            disabled={disabled}
            rows={4}
            placeholder={`输入 ${key} 阶段的 Prompt 模板（支持 Jinja2）`}
          />
        </Card>
      ))}
      {!disabled && (
        <Space style={{ marginTop: 8 }}>
          <Input
            value={newKey}
            onChange={(e) => setNewKey(e.target.value)}
            onPressEnter={addKey}
            placeholder="自定义 Prompt 键名"
            style={{ width: 250 }}
          />
          <Button type="dashed" icon={<PlusOutlined />} onClick={addKey}>
            添加自定义 Prompt
          </Button>
        </Space>
      )}
    </Space>
  );
}
