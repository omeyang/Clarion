import { Button, Input, Select, Switch, Table, Popconfirm } from 'antd';
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons';

interface FieldItem {
  name: string;
  type: string;
  label: string;
  required: boolean;
}

export interface ExtractionSchema {
  fields: FieldItem[];
}

interface Props {
  value: ExtractionSchema | null;
  onChange: (val: ExtractionSchema) => void;
  disabled?: boolean;
}

const TYPE_OPTIONS = [
  { label: 'string', value: 'string' },
  { label: 'number', value: 'number' },
  { label: 'boolean', value: 'boolean' },
  { label: 'array', value: 'array' },
];

export default function ExtractionSchemaEditor({ value, onChange, disabled }: Props) {
  const config = value ?? { fields: [] };

  const updateField = (index: number, field: keyof FieldItem, val: string | boolean) => {
    const fields = [...config.fields];
    fields[index] = { ...fields[index], [field]: val };
    onChange({ fields });
  };

  const addField = () => {
    onChange({ fields: [...config.fields, { name: '', type: 'string', label: '', required: false }] });
  };

  const removeField = (index: number) => {
    onChange({ fields: config.fields.filter((_, i) => i !== index) });
  };

  return (
    <div>
      <Table
        dataSource={config.fields.map((f, i) => ({ ...f, _key: i }))}
        rowKey="_key"
        pagination={false}
        size="small"
        columns={[
          {
            title: '字段名',
            dataIndex: 'name',
            width: 150,
            render: (text: string, _: unknown, index: number) => (
              <Input value={text} onChange={(e) => updateField(index, 'name', e.target.value)} disabled={disabled} placeholder="name" />
            ),
          },
          {
            title: '类型',
            dataIndex: 'type',
            width: 120,
            render: (text: string, _: unknown, index: number) => (
              <Select
                value={text}
                onChange={(val) => updateField(index, 'type', val)}
                disabled={disabled}
                style={{ width: '100%' }}
                options={TYPE_OPTIONS}
              />
            ),
          },
          {
            title: '显示名',
            dataIndex: 'label',
            render: (text: string, _: unknown, index: number) => (
              <Input value={text} onChange={(e) => updateField(index, 'label', e.target.value)} disabled={disabled} placeholder="姓名" />
            ),
          },
          {
            title: '必填',
            dataIndex: 'required',
            width: 80,
            align: 'center' as const,
            render: (checked: boolean, _: unknown, index: number) => (
              <Switch checked={checked} onChange={(val) => updateField(index, 'required', val)} disabled={disabled} size="small" />
            ),
          },
          ...(!disabled
            ? [
                {
                  title: '操作',
                  width: 60,
                  render: (_: unknown, __: unknown, index: number) => (
                    <Popconfirm title="确认删除?" onConfirm={() => removeField(index)}>
                      <Button type="link" danger icon={<DeleteOutlined />} size="small" />
                    </Popconfirm>
                  ),
                },
              ]
            : []),
        ]}
      />
      {!disabled && (
        <Button type="dashed" icon={<PlusOutlined />} onClick={addField} style={{ marginTop: 8 }} block>
          添加字段
        </Button>
      )}
    </div>
  );
}
