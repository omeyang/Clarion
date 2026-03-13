import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Card, Collapse, Descriptions, Button, Space, Spin, Tabs, Input, message, Tag, Timeline } from 'antd';
import { ArrowLeftOutlined, SaveOutlined, SendOutlined } from '@ant-design/icons';
import { getTemplate, updateTemplate, publishTemplate, listSnapshots } from '@/api/templates';
import type { Template, Snapshot } from '@/types/api';
import dayjs from 'dayjs';
import {
  StateMachineEditor,
  ExtractionSchemaEditor,
  GradingRulesEditor,
  PromptTemplatesEditor,
  NotificationConfigEditor,
  CallProtectionEditor,
} from './editors';
import type {
  StateMachineConfig,
  ExtractionSchema,
  GradingRules,
  PromptTemplates,
  NotificationConfig,
  CallProtectionConfig,
} from './editors';

export default function TemplateDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [template, setTemplate] = useState<Template | null>(null);
  const [snapshots, setSnapshots] = useState<Snapshot[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  // Editable fields
  const [name, setName] = useState('');
  const [openingScript, setOpeningScript] = useState('');

  // Config editors state
  const [stateMachineConfig, setStateMachineConfig] = useState<StateMachineConfig | null>(null);
  const [extractionSchema, setExtractionSchema] = useState<ExtractionSchema | null>(null);
  const [gradingRules, setGradingRules] = useState<GradingRules | null>(null);
  const [promptTemplates, setPromptTemplates] = useState<PromptTemplates | null>(null);
  const [notificationConfig, setNotificationConfig] = useState<NotificationConfig | null>(null);
  const [callProtectionConfig, setCallProtectionConfig] = useState<CallProtectionConfig | null>(null);

  const load = async () => {
    if (!id) return;
    setLoading(true);
    try {
      const [t, snaps] = await Promise.all([
        getTemplate(Number(id)),
        listSnapshots(Number(id)),
      ]);
      setTemplate(t);
      setSnapshots(snaps);
      setName(t.name);
      setOpeningScript(t.opening_script || '');
      setStateMachineConfig(t.state_machine_config as StateMachineConfig | null);
      setExtractionSchema(t.extraction_schema as ExtractionSchema | null);
      setGradingRules(t.grading_rules as GradingRules | null);
      setPromptTemplates(t.prompt_templates as PromptTemplates | null);
      setNotificationConfig(t.notification_config as NotificationConfig | null);
      setCallProtectionConfig(t.call_protection_config as CallProtectionConfig | null);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, [id]);

  const handleSave = async () => {
    if (!id) return;
    setSaving(true);
    try {
      const updated = await updateTemplate(Number(id), {
        name: name.trim(),
        opening_script: openingScript.trim() || undefined,
        state_machine_config: stateMachineConfig as Record<string, unknown> | undefined,
        extraction_schema: extractionSchema as Record<string, unknown> | undefined,
        grading_rules: gradingRules as Record<string, unknown> | undefined,
        prompt_templates: promptTemplates as Record<string, unknown> | undefined,
        notification_config: notificationConfig as Record<string, unknown> | undefined,
        call_protection_config: callProtectionConfig as Record<string, unknown> | undefined,
      });
      setTemplate(updated);
      message.success('已保存');
    } finally {
      setSaving(false);
    }
  };

  const handlePublish = async () => {
    if (!id) return;
    await publishTemplate(Number(id));
    message.success('发布成功');
    load();
  };

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;
  if (!template) return <div>模板不存在</div>;

  const isDraft = template.status === 'draft';

  const configItems = [
    {
      key: 'state_machine',
      label: '状态机配置',
      children: (
        <StateMachineEditor value={stateMachineConfig} onChange={setStateMachineConfig} disabled={!isDraft} />
      ),
    },
    {
      key: 'extraction',
      label: '字段提取 Schema',
      children: (
        <ExtractionSchemaEditor value={extractionSchema} onChange={setExtractionSchema} disabled={!isDraft} />
      ),
    },
    {
      key: 'grading',
      label: '分级规则',
      children: (
        <GradingRulesEditor value={gradingRules} onChange={setGradingRules} disabled={!isDraft} />
      ),
    },
    {
      key: 'prompts',
      label: 'Prompt 模板',
      children: (
        <PromptTemplatesEditor value={promptTemplates} onChange={setPromptTemplates} disabled={!isDraft} />
      ),
    },
    {
      key: 'notification',
      label: '通知配置',
      children: (
        <NotificationConfigEditor value={notificationConfig} onChange={setNotificationConfig} disabled={!isDraft} />
      ),
    },
    {
      key: 'protection',
      label: '通话保护配置',
      children: (
        <CallProtectionEditor value={callProtectionConfig} onChange={setCallProtectionConfig} disabled={!isDraft} />
      ),
    },
  ];

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/templates')}>
          返回列表
        </Button>
        {isDraft && (
          <>
            <Button type="primary" icon={<SaveOutlined />} loading={saving} onClick={handleSave}>
              保存
            </Button>
            <Button icon={<SendOutlined />} onClick={handlePublish}>
              发布
            </Button>
          </>
        )}
      </Space>

      <Tabs
        items={[
          {
            key: 'basic',
            label: '基本信息',
            children: (
              <Card>
                <Descriptions column={2} bordered size="small">
                  <Descriptions.Item label="ID">{template.id}</Descriptions.Item>
                  <Descriptions.Item label="状态">
                    <Tag color={isDraft ? 'default' : 'green'}>
                      {isDraft ? '草稿' : '已发布'}
                    </Tag>
                  </Descriptions.Item>
                  <Descriptions.Item label="版本">v{template.version}</Descriptions.Item>
                  <Descriptions.Item label="领域">{template.domain}</Descriptions.Item>
                  <Descriptions.Item label="创建时间" span={2}>
                    {dayjs(template.created_at).format('YYYY-MM-DD HH:mm:ss')}
                  </Descriptions.Item>
                </Descriptions>

                <div style={{ marginTop: 24 }}>
                  <label style={{ fontWeight: 500 }}>模板名称</label>
                  <Input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    disabled={!isDraft}
                    style={{ marginTop: 4 }}
                  />
                </div>

                <div style={{ marginTop: 16 }}>
                  <label style={{ fontWeight: 500 }}>开场白话术</label>
                  <Input.TextArea
                    value={openingScript}
                    onChange={(e) => setOpeningScript(e.target.value)}
                    disabled={!isDraft}
                    rows={4}
                    placeholder="您好，我是XX的AI助手..."
                    style={{ marginTop: 4 }}
                  />
                </div>
              </Card>
            ),
          },
          {
            key: 'config',
            label: '配置详情',
            children: (
              <Card>
                <Collapse
                  defaultActiveKey={['state_machine']}
                  items={configItems}
                />
              </Card>
            ),
          },
          {
            key: 'snapshots',
            label: `版本历史 (${snapshots.length})`,
            children: (
              <Card>
                {snapshots.length === 0 ? (
                  <p style={{ color: '#999' }}>暂无发布记录</p>
                ) : (
                  <Timeline
                    items={snapshots.map((s) => ({
                      children: (
                        <div>
                          <strong>快照 #{s.id}</strong>
                          <span style={{ marginLeft: 12, color: '#999' }}>
                            {dayjs(s.created_at).format('YYYY-MM-DD HH:mm:ss')}
                          </span>
                        </div>
                      ),
                    }))}
                  />
                )}
              </Card>
            ),
          },
        ]}
      />
    </div>
  );
}
