import { Button, Card, Input, Space, Tag, Popconfirm } from 'antd';
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons';
import { useState } from 'react';

interface GradeConfig {
  label: string;
  conditions: string[];
}

export interface GradingRules {
  version: string;
  grades: Record<string, GradeConfig>;
}

interface Props {
  value: GradingRules | null;
  onChange: (val: GradingRules) => void;
  disabled?: boolean;
}

const GRADE_KEYS = ['A', 'B', 'C', 'D', 'X'] as const;
const GRADE_COLORS: Record<string, string> = {
  A: 'red',
  B: 'orange',
  C: 'blue',
  D: 'default',
  X: '#999',
};

const DEFAULT_GRADES: Record<string, GradeConfig> = {
  A: { label: '高意向', conditions: [] },
  B: { label: '有意向需跟进', conditions: [] },
  C: { label: '低意向', conditions: [] },
  D: { label: '无意向', conditions: [] },
  X: { label: '无效', conditions: [] },
};

function getDefault(): GradingRules {
  return { version: '1.0', grades: { ...DEFAULT_GRADES } };
}

function GradeSection({
  gradeKey,
  config,
  disabled,
  onChangeLabel,
  onAddCondition,
  onRemoveCondition,
}: {
  gradeKey: string;
  config: GradeConfig;
  disabled?: boolean;
  onChangeLabel: (label: string) => void;
  onAddCondition: (condition: string) => void;
  onRemoveCondition: (index: number) => void;
}) {
  const [newCondition, setNewCondition] = useState('');

  const handleAdd = () => {
    const trimmed = newCondition.trim();
    if (trimmed) {
      onAddCondition(trimmed);
      setNewCondition('');
    }
  };

  return (
    <Card
      size="small"
      title={
        <Space>
          <Tag color={GRADE_COLORS[gradeKey]}>{gradeKey} 级</Tag>
          <Input
            value={config.label}
            onChange={(e) => onChangeLabel(e.target.value)}
            disabled={disabled}
            style={{ width: 200 }}
            placeholder="等级描述"
          />
        </Space>
      }
    >
      <Space direction="vertical" style={{ width: '100%' }} size="small">
        {config.conditions.map((cond, i) => (
          <Space key={i} style={{ width: '100%' }}>
            <Input value={cond} disabled style={{ width: 400 }} />
            {!disabled && (
              <Popconfirm title="确认删除?" onConfirm={() => onRemoveCondition(i)}>
                <Button type="link" danger icon={<DeleteOutlined />} size="small" />
              </Popconfirm>
            )}
          </Space>
        ))}
        {!disabled && (
          <Space>
            <Input
              value={newCondition}
              onChange={(e) => setNewCondition(e.target.value)}
              onPressEnter={handleAdd}
              placeholder="输入条件表达式，回车添加"
              style={{ width: 400 }}
            />
            <Button type="dashed" icon={<PlusOutlined />} onClick={handleAdd} size="small">
              添加
            </Button>
          </Space>
        )}
      </Space>
    </Card>
  );
}

export default function GradingRulesEditor({ value, onChange, disabled }: Props) {
  const config = value ?? getDefault();
  const grades = config.grades ?? {};

  const updateGrade = (key: string, updated: GradeConfig) => {
    onChange({ ...config, grades: { ...grades, [key]: updated } });
  };

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="small">
      {GRADE_KEYS.map((key) => {
        const grade = grades[key] ?? { label: '', conditions: [] };
        return (
          <GradeSection
            key={key}
            gradeKey={key}
            config={grade}
            disabled={disabled}
            onChangeLabel={(label) => updateGrade(key, { ...grade, label })}
            onAddCondition={(cond) => updateGrade(key, { ...grade, conditions: [...grade.conditions, cond] })}
            onRemoveCondition={(i) =>
              updateGrade(key, { ...grade, conditions: grade.conditions.filter((_, idx) => idx !== i) })
            }
          />
        );
      })}
    </Space>
  );
}
