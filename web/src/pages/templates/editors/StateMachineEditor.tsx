import { Button, Card, Input, Space, Table, Select, Popconfirm } from 'antd';
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons';

interface StateItem {
  name: string;
  label: string;
  description: string;
}

interface TransitionItem {
  from: string;
  to: string;
  condition: string;
}

export interface StateMachineConfig {
  states: StateItem[];
  transitions: TransitionItem[];
}

interface Props {
  value: StateMachineConfig | null;
  onChange: (val: StateMachineConfig) => void;
  disabled?: boolean;
}

const DEFAULT_STATES: StateItem[] = [
  { name: 'Opening', label: '开场', description: '表明身份，确认是否方便沟通' },
  { name: 'Qualification', label: '资质判断', description: '判断是否值得继续沟通' },
  { name: 'InformationGathering', label: '信息收集', description: '收集关键信息' },
  { name: 'ObjectionHandling', label: '异议处理', description: '处理客户疑虑' },
  { name: 'NextAction', label: '下一步', description: '确定后续行动' },
  { name: 'MarkForFollowup', label: '标记跟进', description: '标记需要跟进' },
  { name: 'Closing', label: '结束', description: '结束通话' },
];

function getDefault(): StateMachineConfig {
  return { states: [...DEFAULT_STATES], transitions: [] };
}

export default function StateMachineEditor({ value, onChange, disabled }: Props) {
  const config = value ?? getDefault();
  const stateNames = config.states.map((s) => s.name);

  const updateState = (index: number, field: keyof StateItem, val: string) => {
    const states = [...config.states];
    states[index] = { ...states[index], [field]: val };
    onChange({ ...config, states });
  };

  const addState = () => {
    onChange({ ...config, states: [...config.states, { name: '', label: '', description: '' }] });
  };

  const removeState = (index: number) => {
    const states = config.states.filter((_, i) => i !== index);
    onChange({ ...config, states });
  };

  const updateTransition = (index: number, field: keyof TransitionItem, val: string) => {
    const transitions = [...config.transitions];
    transitions[index] = { ...transitions[index], [field]: val };
    onChange({ ...config, transitions });
  };

  const addTransition = () => {
    onChange({
      ...config,
      transitions: [...config.transitions, { from: '', to: '', condition: '' }],
    });
  };

  const removeTransition = (index: number) => {
    const transitions = config.transitions.filter((_, i) => i !== index);
    onChange({ ...config, transitions });
  };

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <Card title="状态列表" size="small">
        <Table
          dataSource={config.states.map((s, i) => ({ ...s, _key: i }))}
          rowKey="_key"
          pagination={false}
          size="small"
          columns={[
            {
              title: '状态名',
              dataIndex: 'name',
              width: 180,
              render: (text: string, _: unknown, index: number) => (
                <Input value={text} onChange={(e) => updateState(index, 'name', e.target.value)} disabled={disabled} placeholder="Opening" />
              ),
            },
            {
              title: '显示名',
              dataIndex: 'label',
              width: 120,
              render: (text: string, _: unknown, index: number) => (
                <Input value={text} onChange={(e) => updateState(index, 'label', e.target.value)} disabled={disabled} placeholder="开场" />
              ),
            },
            {
              title: '描述',
              dataIndex: 'description',
              render: (text: string, _: unknown, index: number) => (
                <Input value={text} onChange={(e) => updateState(index, 'description', e.target.value)} disabled={disabled} />
              ),
            },
            ...(!disabled
              ? [
                  {
                    title: '操作',
                    width: 60,
                    render: (_: unknown, __: unknown, index: number) => (
                      <Popconfirm title="确认删除?" onConfirm={() => removeState(index)}>
                        <Button type="link" danger icon={<DeleteOutlined />} size="small" />
                      </Popconfirm>
                    ),
                  },
                ]
              : []),
          ]}
        />
        {!disabled && (
          <Button type="dashed" icon={<PlusOutlined />} onClick={addState} style={{ marginTop: 8 }} block>
            添加状态
          </Button>
        )}
      </Card>

      <Card title="转换规则" size="small">
        <Table
          dataSource={config.transitions.map((t, i) => ({ ...t, _key: i }))}
          rowKey="_key"
          pagination={false}
          size="small"
          columns={[
            {
              title: '来源状态',
              dataIndex: 'from',
              width: 180,
              render: (text: string, _: unknown, index: number) => (
                <Select
                  value={text || undefined}
                  onChange={(val) => updateTransition(index, 'from', val)}
                  disabled={disabled}
                  placeholder="选择状态"
                  style={{ width: '100%' }}
                  options={stateNames.map((n) => ({ label: n, value: n }))}
                />
              ),
            },
            {
              title: '目标状态',
              dataIndex: 'to',
              width: 180,
              render: (text: string, _: unknown, index: number) => (
                <Select
                  value={text || undefined}
                  onChange={(val) => updateTransition(index, 'to', val)}
                  disabled={disabled}
                  placeholder="选择状态"
                  style={{ width: '100%' }}
                  options={stateNames.map((n) => ({ label: n, value: n }))}
                />
              ),
            },
            {
              title: '条件',
              dataIndex: 'condition',
              render: (text: string, _: unknown, index: number) => (
                <Input
                  value={text}
                  onChange={(e) => updateTransition(index, 'condition', e.target.value)}
                  disabled={disabled}
                  placeholder="willing_to_continue"
                />
              ),
            },
            ...(!disabled
              ? [
                  {
                    title: '操作',
                    width: 60,
                    render: (_: unknown, __: unknown, index: number) => (
                      <Popconfirm title="确认删除?" onConfirm={() => removeTransition(index)}>
                        <Button type="link" danger icon={<DeleteOutlined />} size="small" />
                      </Popconfirm>
                    ),
                  },
                ]
              : []),
          ]}
        />
        {!disabled && (
          <Button type="dashed" icon={<PlusOutlined />} onClick={addTransition} style={{ marginTop: 8 }} block>
            添加转换规则
          </Button>
        )}
      </Card>
    </Space>
  );
}
