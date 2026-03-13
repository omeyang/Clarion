import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Card, Descriptions, Button, Spin, Tag, Tabs, Timeline } from 'antd';
import { ArrowLeftOutlined, UserOutlined, RobotOutlined } from '@ant-design/icons';
import { getCall, getCallTurns, getCallEvents } from '@/api/calls';
import type { Call, DialogueTurn, CallEvent } from '@/types/api';
import dayjs from 'dayjs';

export default function CallDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [call, setCall] = useState<Call | null>(null);
  const [turns, setTurns] = useState<DialogueTurn[]>([]);
  const [events, setEvents] = useState<CallEvent[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!id) return;
    setLoading(true);
    Promise.all([
      getCall(Number(id)),
      getCallTurns(Number(id)),
      getCallEvents(Number(id)),
    ]).then(([c, t, e]) => {
      setCall(c);
      setTurns(t);
      setEvents(e);
    }).finally(() => setLoading(false));
  }, [id]);

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;
  if (!call) return <div>通话记录不存在</div>;

  return (
    <div>
      <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/calls')} style={{ marginBottom: 16 }}>
        返回列表
      </Button>

      <Tabs items={[
        {
          key: 'info',
          label: '通话信息',
          children: (
            <Card>
              <Descriptions bordered column={2} size="small">
                <Descriptions.Item label="ID">{call.id}</Descriptions.Item>
                <Descriptions.Item label="会话ID">{call.session_id}</Descriptions.Item>
                <Descriptions.Item label="状态"><Tag>{call.status}</Tag></Descriptions.Item>
                <Descriptions.Item label="接听类型">{call.answer_type || '-'}</Descriptions.Item>
                <Descriptions.Item label="通话时长">{call.duration ? `${call.duration}s` : '-'}</Descriptions.Item>
                <Descriptions.Item label="意向等级">
                  {call.result_grade ? <Tag color="blue">{call.result_grade}</Tag> : '-'}
                </Descriptions.Item>
                <Descriptions.Item label="下一步动作">{call.next_action || '-'}</Descriptions.Item>
                <Descriptions.Item label="联系人ID">{call.contact_id}</Descriptions.Item>
                <Descriptions.Item label="任务ID">{call.task_id}</Descriptions.Item>
                <Descriptions.Item label="创建时间">{dayjs(call.created_at).format('YYYY-MM-DD HH:mm:ss')}</Descriptions.Item>
              </Descriptions>

              {call.ai_summary && (
                <div style={{ marginTop: 16 }}>
                  <h4>AI 摘要</h4>
                  <Card size="small" style={{ background: '#f6ffed' }}>{call.ai_summary}</Card>
                </div>
              )}

              {call.extracted_fields && (
                <div style={{ marginTop: 16 }}>
                  <h4>提取字段</h4>
                  <pre style={{ background: '#f5f5f5', padding: 12, borderRadius: 4 }}>
                    {JSON.stringify(call.extracted_fields, null, 2)}
                  </pre>
                </div>
              )}
            </Card>
          ),
        },
        {
          key: 'dialogue',
          label: `对话记录 (${turns.length})`,
          children: (
            <Card>
              {turns.length === 0 ? (
                <p style={{ color: '#999' }}>暂无对话记录</p>
              ) : (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                  {turns.map((turn) => (
                    <div
                      key={turn.id}
                      style={{
                        display: 'flex',
                        justifyContent: turn.speaker === 'ai' ? 'flex-start' : 'flex-end',
                      }}
                    >
                      <div
                        style={{
                          maxWidth: '70%',
                          padding: '8px 16px',
                          borderRadius: 12,
                          background: turn.speaker === 'ai' ? '#f0f5ff' : '#e6fffb',
                          border: `1px solid ${turn.speaker === 'ai' ? '#adc6ff' : '#87e8de'}`,
                        }}
                      >
                        <div style={{ fontSize: 12, color: '#999', marginBottom: 4 }}>
                          {turn.speaker === 'ai' ? <><RobotOutlined /> AI</> : <><UserOutlined /> 用户</>}
                          {' '}
                          #{turn.turn_number}
                          {turn.is_interrupted && <Tag color="orange" style={{ marginLeft: 4 }}>被打断</Tag>}
                        </div>
                        <div>{turn.content}</div>
                        {(turn.asr_latency_ms || turn.llm_latency_ms || turn.tts_latency_ms) && (
                          <div style={{ fontSize: 11, color: '#bbb', marginTop: 4 }}>
                            {turn.asr_latency_ms && `ASR: ${turn.asr_latency_ms}ms `}
                            {turn.llm_latency_ms && `LLM: ${turn.llm_latency_ms}ms `}
                            {turn.tts_latency_ms && `TTS: ${turn.tts_latency_ms}ms`}
                          </div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </Card>
          ),
        },
        {
          key: 'events',
          label: `事件流 (${events.length})`,
          children: (
            <Card>
              {events.length === 0 ? (
                <p style={{ color: '#999' }}>暂无事件</p>
              ) : (
                <Timeline
                  items={events.map((e) => ({
                    color: e.event_type.includes('error') ? 'red' : 'blue',
                    children: (
                      <div>
                        <Tag>{e.event_type}</Tag>
                        <span style={{ color: '#999', marginLeft: 8 }}>
                          {dayjs(e.created_at).format('HH:mm:ss.SSS')}
                        </span>
                        {e.metadata_json && (
                          <pre style={{ fontSize: 11, marginTop: 4, color: '#666' }}>
                            {JSON.stringify(e.metadata_json, null, 2)}
                          </pre>
                        )}
                      </div>
                    ),
                  }))}
                />
              )}
            </Card>
          ),
        },
      ]} />
    </div>
  );
}
