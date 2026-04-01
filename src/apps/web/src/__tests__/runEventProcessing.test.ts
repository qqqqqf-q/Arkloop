import { describe, expect, it } from 'vitest'
import { ACP_DELEGATE_LAYER } from '@arkloop/shared'
import {
  buildMessageFileOpsFromRunEvents,
  buildMessageCodeExecutionsFromRunEvents,
  buildMessageSubAgentsFromRunEvents,
  buildMessageThinkingFromRunEvents,
  buildMessageWidgetsFromRunEvents,
  findAssistantMessageForRun,
  patchCodeExecutionList,
  selectFreshRunEvents,
  runEventDismissesAssistantPlaceholder,
  shouldRefetchCompletedRunMessages,
  shouldReplayMessageCodeExecutions,
  fileOpOutputFromResult,
  applyWebFetchToolCall,
  applyWebFetchToolResult,
  isWebFetchToolName,
} from '../runEventProcessing'
import type { MessageResponse } from '../api'
import type { RunEvent } from '../sse'

function makeRunEvent(params: {
  runId: string
  seq: number
  type: string
  data?: unknown
  errorClass?: string
}): RunEvent {
  return {
    event_id: `evt_${params.seq}`,
    run_id: params.runId,
    seq: params.seq,
    ts: '2024-01-01T00:00:00.000Z',
    type: params.type,
    data: params.data ?? {},
    error_class: params.errorClass,
  }
}

function makeMessage(params: {
  id: string
  role: string
  content: string
  runId?: string
}): MessageResponse {
  return {
    id: params.id,
    account_id: 'acc_1',
    thread_id: 'thread_1',
    created_by_user_id: 'user_1',
    role: params.role,
    content: params.content,
    created_at: '2026-01-01T00:00:00.000Z',
    run_id: params.runId,
  }
}

describe('selectFreshRunEvents', () => {
  it('应忽略旧 run 的尾部事件，避免误触发断开', () => {
    const events = [makeRunEvent({ runId: 'run_1', seq: 1, type: 'run.completed' })]

    const result = selectFreshRunEvents({
      events,
      activeRunId: 'run_2',
      processedCount: 0,
    })

    expect(result.fresh).toEqual([])
    expect(result.nextProcessedCount).toBe(1)
  })

  it('应在 events 被清空后重置 processedCount', () => {
    const result = selectFreshRunEvents({
      events: [],
      activeRunId: 'run_1',
      processedCount: 10,
    })

    expect(result.fresh).toEqual([])
    expect(result.nextProcessedCount).toBe(0)
  })

  it('应只返回当前 run 的新事件，并推进游标到末尾', () => {
    const events = [
      makeRunEvent({ runId: 'run_1', seq: 1, type: 'run.started' }),
      makeRunEvent({
        runId: 'run_2',
        seq: 2,
        type: 'message.delta',
        data: { content_delta: 'hi', role: 'assistant' },
      }),
      makeRunEvent({ runId: 'run_2', seq: 3, type: 'run.completed' }),
    ]

    const result = selectFreshRunEvents({
      events,
      activeRunId: 'run_2',
      processedCount: 0,
    })

    expect(result.fresh.map((item) => item.seq)).toEqual([2, 3])
    expect(result.nextProcessedCount).toBe(3)
  })

  it('应从 processedCount 之后开始取新事件', () => {
    const events = [
      makeRunEvent({ runId: 'run_1', seq: 1, type: 'run.started' }),
      makeRunEvent({ runId: 'run_1', seq: 2, type: 'message.delta' }),
    ]

    const result = selectFreshRunEvents({
      events,
      activeRunId: 'run_1',
      processedCount: 1,
    })

    expect(result.fresh.map((item) => item.seq)).toEqual([2])
    expect(result.nextProcessedCount).toBe(2)
  })

  it('processedCount 超过 events.length 时重置为 0', () => {
    const events = [
      makeRunEvent({ runId: 'run_1', seq: 1, type: 'run.started' }),
      makeRunEvent({ runId: 'run_1', seq: 2, type: 'message.delta' }),
    ]

    const result = selectFreshRunEvents({
      events,
      activeRunId: 'run_1',
      processedCount: 999,
    })

    expect(result.fresh.map((item) => item.seq)).toEqual([1, 2])
    expect(result.nextProcessedCount).toBe(2)
  })
})

describe('isWebFetchToolName', () => {
  it('接受常见 fetch 命名变体', () => {
    expect(isWebFetchToolName('web_fetch')).toBe(true)
    expect(isWebFetchToolName('webfetch')).toBe(true)
    expect(isWebFetchToolName('web-fetch')).toBe(true)
    expect(isWebFetchToolName('web_fetch.jina')).toBe(true)
    expect(isWebFetchToolName('fetch_url')).toBe(false)
  })
})

describe('web fetch processing', () => {
  it('支持 provider 变体名称', () => {
    const call = makeRunEvent({
      runId: 'run_1',
      seq: 1,
      type: 'tool.call',
      data: {
        tool_name: 'web_fetch.jina',
        tool_call_id: 'wf_1',
        arguments: { url: 'https://example.com' },
      },
    })
    const result = makeRunEvent({
      runId: 'run_1',
      seq: 2,
      type: 'tool.result',
      data: {
        tool_name: 'web_fetch.jina',
        tool_call_id: 'wf_1',
        result: { title: 'Example', status_code: 200 },
      },
    })

    const afterCall = applyWebFetchToolCall([], call)
    expect(afterCall.nextFetches).toEqual([
      { id: 'wf_1', url: 'https://example.com', status: 'fetching', seq: 1 },
    ])
    const afterResult = applyWebFetchToolResult(afterCall.nextFetches, result)
    expect(afterResult.nextFetches).toEqual([
      { id: 'wf_1', url: 'https://example.com', title: 'Example', statusCode: 200, status: 'done', seq: 1 },
    ])
  })
})

describe('runEventDismissesAssistantPlaceholder', () => {
  it('segment / 空 delta 不关闭占位', () => {
    expect(
      runEventDismissesAssistantPlaceholder(
        makeRunEvent({
          runId: 'r1',
          seq: 1,
          type: 'run.segment.start',
          data: { segment_id: 's1', kind: 'planning_round', display: { label: 'x' } },
        }),
      ),
    ).toBe(false)
    expect(
      runEventDismissesAssistantPlaceholder(
        makeRunEvent({
          runId: 'r1',
          seq: 2,
          type: 'message.delta',
          data: { content_delta: '', role: 'assistant' },
        }),
      ),
    ).toBe(false)
    expect(
      runEventDismissesAssistantPlaceholder(
        makeRunEvent({
          runId: 'r1',
          seq: 3,
          type: 'message.delta',
          data: { content_delta: 'int', role: 'assistant', channel: 'thinking' },
        }),
      ),
    ).toBe(false)
  })

  it('助手正文 delta 与 tool 事件关闭占位', () => {
    expect(
      runEventDismissesAssistantPlaceholder(
        makeRunEvent({
          runId: 'r1',
          seq: 1,
          type: 'message.delta',
          data: { content_delta: 'hi', role: 'assistant' },
        }),
      ),
    ).toBe(true)
    expect(
      runEventDismissesAssistantPlaceholder(makeRunEvent({ runId: 'r1', seq: 2, type: 'tool.call', data: {} })),
    ).toBe(true)
    expect(
      runEventDismissesAssistantPlaceholder(
        makeRunEvent({ runId: 'r1', seq: 3, type: 'tool.call.delta', data: { tool_call_index: 0, arguments_delta: '{' } }),
      ),
    ).toBe(true)
  })

  it('ACP delegate 的 delta / tool 不关闭占位', () => {
    const d = { delegate_layer: ACP_DELEGATE_LAYER }
    expect(
      runEventDismissesAssistantPlaceholder(
        makeRunEvent({
          runId: 'r1',
          seq: 1,
          type: 'message.delta',
          data: { ...d, content_delta: 'inner', role: 'assistant' },
        }),
      ),
    ).toBe(false)
    expect(
      runEventDismissesAssistantPlaceholder(
        makeRunEvent({ runId: 'r1', seq: 2, type: 'tool.call', data: { ...d, tool_name: 'read_file' } }),
      ),
    ).toBe(false)
  })
})

describe('completed run message sync', () => {
  it('应按 run_id 命中对应 assistant 消息', () => {
    const messages = [
      makeMessage({ id: 'm1', role: 'assistant', content: '旧回答', runId: 'run_1' }),
      makeMessage({ id: 'm2', role: 'assistant', content: '新回答', runId: 'run_2' }),
    ]

    expect(findAssistantMessageForRun(messages, 'run_2')?.id).toBe('m2')
    expect(findAssistantMessageForRun(messages, 'run_x')).toBeUndefined()
  })

  it('completed 但缺少对应 assistant 消息时应触发补拉', () => {
    const messages = [
      makeMessage({ id: 'm1', role: 'user', content: '问题' }),
      makeMessage({ id: 'm2', role: 'assistant', content: '旧回答', runId: 'run_1' }),
    ]

    expect(shouldRefetchCompletedRunMessages({
      messages,
      latestRun: { run_id: 'run_2', status: 'completed' },
    })).toBe(true)
  })

  it('completed 且已包含对应 assistant 消息时不应补拉', () => {
    const messages = [
      makeMessage({ id: 'm1', role: 'assistant', content: '回答', runId: 'run_2' }),
    ]

    expect(shouldRefetchCompletedRunMessages({
      messages,
      latestRun: { run_id: 'run_2', status: 'completed' },
    })).toBe(false)
  })
})

describe('read tool provider mapping', () => {
  it('应将 read 工具调用按 read_file 样式写入 file ops', () => {
    const events = [
      makeRunEvent({
        runId: 'r1',
        seq: 1,
        type: 'tool.call',
        data: {
          tool_name: 'read',
          tool_call_id: 'c1',
          arguments: {
            source: { kind: 'file_path', file_path: '/tmp/demo.txt' },
          },
        },
      }),
      makeRunEvent({
        runId: 'r1',
        seq: 2,
        type: 'tool.result',
        data: {
          tool_name: 'read',
          tool_call_id: 'c1',
          result: { content: 'hello' },
        },
      }),
    ]

    expect(buildMessageFileOpsFromRunEvents(events)).toEqual([
      {
        id: 'c1',
        toolName: 'read_file',
        label: 'Read demo.txt',
        status: 'success',
        seq: 1,
        output: 'hello',
      },
    ])
  })

  it('应将 read.minimax 工具调用按 read_file 样式写入 file ops', () => {
    const events = [
      makeRunEvent({
        runId: 'r1',
        seq: 1,
        type: 'tool.call',
        data: {
          tool_name: 'read.minimax',
          tool_call_id: 'c2',
          arguments: {
            source: { kind: 'file_path', file_path: '/tmp/demo.txt' },
          },
        },
      }),
      makeRunEvent({
        runId: 'r1',
        seq: 2,
        type: 'tool.result',
        data: {
          tool_name: 'read',
          tool_call_id: 'c2',
          result: { content: 'hello' },
        },
      }),
    ]

    expect(buildMessageFileOpsFromRunEvents(events)).toEqual([
      {
        id: 'c2',
        toolName: 'read_file',
        label: 'Read demo.txt',
        status: 'success',
        seq: 1,
        output: 'hello',
      },
    ])
  })
})

describe('buildMessageWidgetsFromRunEvents', () => {
  it('应从 show_widget 的 tool.call 事件恢复 widget', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: {
          tool_name: 'show_widget',
          tool_call_id: 'call_widget',
          arguments: {
            title: '销售图表',
            widget_code: '<div id="chart">ok</div>',
          },
        },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.result',
        data: {
          tool_name: 'show_widget',
          tool_call_id: 'call_widget',
          result: { ok: true },
        },
      }),
    ]

    expect(buildMessageWidgetsFromRunEvents(events)).toEqual([
      {
        id: 'call_widget',
        title: '销售图表',
        html: '<div id="chart">ok</div>',
      },
    ])
  })

  it('应忽略缺少 widget_code 的 show_widget 事件和重复 call id', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: {
          tool_name: 'show_widget',
          tool_call_id: 'call_widget',
          arguments: {
            title: '默认标题',
            widget_code: '<div>first</div>',
          },
        },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.call',
        data: {
          tool_name: 'show_widget',
          tool_call_id: 'call_widget',
          arguments: {
            title: '重复调用',
            widget_code: '<div>second</div>',
          },
        },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 3,
        type: 'tool.call',
        data: {
          tool_name: 'show_widget',
          tool_call_id: 'call_missing',
          arguments: {
            title: '缺内容',
          },
        },
      }),
    ]

    expect(buildMessageWidgetsFromRunEvents(events)).toEqual([
      {
        id: 'call_widget',
        title: '默认标题',
        html: '<div>first</div>',
      },
    ])
  })
})

describe('buildMessageCodeExecutionsFromRunEvents', () => {
  it('应将 write_stdin 结果回填到原始 exec_command 记录', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: { tool_name: 'exec_command', tool_call_id: 'call_exec', arguments: { command: 'uname -a' } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.result',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call_exec',
          result: { session_id: 'sess_1', running: true, output: 'Linux ' },
        },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 3,
        type: 'tool.call',
        data: { tool_name: 'write_stdin', tool_call_id: 'call_write', arguments: { session_id: 'sess_1' } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 4,
        type: 'tool.result',
        data: {
          tool_name: 'write_stdin',
          tool_call_id: 'call_write',
          result: { session_id: 'sess_1', running: false, output: '6.12.72', exit_code: 0 },
        },
      }),
    ]

    const executions = buildMessageCodeExecutionsFromRunEvents(events)
    expect(executions).toHaveLength(1)
    expect(executions[0]).toMatchObject({
      id: 'call_exec',
      language: 'shell',
      code: 'uname -a',
      sessionId: 'sess_1',
      output: 'Linux 6.12.72',
      exitCode: 0,
      status: 'success',
    })
  })

  it('应过滤常见终端控制序列', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: { tool_name: 'exec_command', tool_call_id: 'call_exec', arguments: { command: 'uname -a' } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.result',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call_exec',
          result: { session_id: 'sess_1', running: false, output: '\u001b[?2004hLinux\n\u001b[?2004l', exit_code: 0 },
        },
      }),
    ]

    const executions = buildMessageCodeExecutionsFromRunEvents(events)
    expect(executions).toHaveLength(1)
    expect(executions[0]?.output).toBe('Linux\n')
    expect(executions[0]?.status).toBe('success')
  })

  it('累计输出与全量输出混用时应避免重复拼接', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: { tool_name: 'exec_command', tool_call_id: 'call_exec', arguments: { command: 'echo hi' } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.result',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call_exec',
          result: { session_id: 'sess_1', running: true, output: 'hi' },
        },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 3,
        type: 'tool.result',
        data: {
          tool_name: 'write_stdin',
          tool_call_id: 'call_write',
          result: { session_id: 'sess_1', running: false, output: 'hi there', exit_code: 0 },
        },
      }),
    ]

    const executions = buildMessageCodeExecutionsFromRunEvents(events)
    expect(executions).toHaveLength(1)
    expect(executions[0]?.output).toBe('hi there')
    expect(executions[0]?.status).toBe('success')
  })

  it('缺少原始 exec_command 时，write_stdin 结果也不应丢失', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.result',
        data: {
          tool_name: 'write_stdin',
          tool_call_id: 'call_write',
          result: { session_id: 'sess_orphan', running: false, output: 'done', exit_code: 0 },
        },
      }),
    ]

    const executions = buildMessageCodeExecutionsFromRunEvents(events)
    expect(executions).toHaveLength(1)
    expect(executions[0]).toMatchObject({
      id: 'call_write',
      language: 'shell',
      sessionId: 'sess_orphan',
      output: 'done',
      exitCode: 0,
      status: 'success',
    })
  })

  it('tool.result 带 error 时应标记为 failed', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: { tool_name: 'exec_command', tool_call_id: 'call_exec', arguments: { command: 'ls -la /workspace/' } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.result',
        errorClass: 'tool.args_invalid',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call_exec',
          error: {
            error_class: 'tool.args_invalid',
            message: 'profile_ref and workspace_ref are required for shell sessions',
          },
        },
      }),
    ]

    const executions = buildMessageCodeExecutionsFromRunEvents(events)
    expect(executions).toHaveLength(1)
    expect(executions[0]).toMatchObject({
      id: 'call_exec',
      language: 'shell',
      status: 'failed',
      errorClass: 'tool.args_invalid',
      errorMessage: 'profile_ref and workspace_ref are required for shell sessions',
    })
  })

  it('无 error 且无 exit_code 的终态结果应标记为 completed', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: { tool_name: 'exec_command', tool_call_id: 'call_exec', arguments: { command: 'ls -la /workspace/' } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.result',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call_exec',
          result: { running: false },
        },
      }),
    ]

    const executions = buildMessageCodeExecutionsFromRunEvents(events)
    expect(executions).toHaveLength(1)
    expect(executions[0]).toMatchObject({
      id: 'call_exec',
      language: 'shell',
      status: 'completed',
    })
  })

  it('终态同时带 running=true 与 exit_code 时应以 exit_code 为准', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: { tool_name: 'exec_command', tool_call_id: 'call_exec', arguments: { command: 'pwd' } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.result',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call_exec',
          result: {
            session_id: 'sess_1',
            running: true,
            output: '/workspace\n',
            exit_code: 0,
          },
        },
      }),
    ]

    const executions = buildMessageCodeExecutionsFromRunEvents(events)
    expect(executions).toHaveLength(1)
    expect(executions[0]).toMatchObject({
      id: 'call_exec',
      language: 'shell',
      status: 'success',
      exitCode: 0,
    })
  })

  it('同一 run 中失败后重试成功时应保留两条状态独立的记录', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: { tool_name: 'exec_command', tool_call_id: 'call_bad', arguments: { command: 'ls -la /workspace/', share_scope: 'run' } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.result',
        errorClass: 'tool.args_invalid',
        data: {
          tool_name: 'exec_command',
          tool_call_id: 'call_bad',
          error: {
            error_class: 'tool.args_invalid',
            message: 'profile_ref and workspace_ref are required for shell sessions',
          },
        },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 3,
        type: 'tool.call',
        data: { tool_name: 'python_execute', tool_call_id: 'call_good', arguments: { code: 'print(1)' } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 4,
        type: 'tool.result',
        data: {
          tool_name: 'python_execute',
          tool_call_id: 'call_good',
          result: { stdout: '1\n', exit_code: 0 },
        },
      }),
    ]

    const executions = buildMessageCodeExecutionsFromRunEvents(events)
    expect(executions).toHaveLength(2)
    expect(executions[0]).toMatchObject({ id: 'call_bad', status: 'failed' })
    expect(executions[1]).toMatchObject({ id: 'call_good', status: 'success' })
  })
})

describe('buildMessageFileOpsFromRunEvents', () => {
  it('应将 load_tools 的 already_active 状态单独汇总', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: { tool_name: 'load_tools', tool_call_id: 'call_search', arguments: { queries: ['web_search'] } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.result',
        data: {
          tool_name: 'load_tools',
          tool_call_id: 'call_search',
          result: {
            matched: [
              { name: 'show_widget' },
              { name: 'web_search', already_active: true },
            ],
          },
        },
      }),
    ]

    const ops = buildMessageFileOpsFromRunEvents(events)
    expect(ops).toHaveLength(1)
    expect(ops[0]?.output).toBe('loaded 1 (show_widget); already active 1 (web_search)')
  })

  it('应在 load_tools 命中均已激活时显示 already active', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: { tool_name: 'load_tools', tool_call_id: 'call_search', arguments: { queries: ['web_search'] } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.result',
        data: {
          tool_name: 'load_tools',
          tool_call_id: 'call_search',
          result: {
            matched: [
              { name: 'web_search', already_active: true },
            ],
          },
        },
      }),
    ]

    const ops = buildMessageFileOpsFromRunEvents(events)
    expect(ops).toHaveLength(1)
    expect(ops[0]?.output).toBe('already active 1 (web_search)')
  })
})

describe('buildMessageSubAgentsFromRunEvents', () => {
  it('应在 spawn_agent 调用开始时创建条目，并在 wait_agent 后收敛为 completed', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: {
          tool_name: 'spawn_agent',
          tool_call_id: 'call_spawn',
          arguments: {
            persona_id: 'normal',
            nickname: 'WikiFetcher',
            context_mode: 'isolated',
            input: '抓取维基百科',
          },
        },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.result',
        data: {
          tool_name: 'spawn_agent',
          tool_call_id: 'call_spawn',
          result: {
            sub_agent_id: 'sub_1',
            status: 'queued',
          },
        },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 3,
        type: 'tool.result',
        data: {
          tool_name: 'wait_agent',
          tool_call_id: 'call_wait',
          result: {
            sub_agent_id: 'sub_1',
            status: 'completed',
            output: '总结完成',
          },
        },
      }),
    ]

    const agents = buildMessageSubAgentsFromRunEvents(events)
    expect(agents).toHaveLength(1)
    expect(agents[0]).toMatchObject({
      id: 'call_spawn',
      subAgentId: 'sub_1',
      nickname: 'WikiFetcher',
      personaId: 'normal',
      contextMode: 'isolated',
      input: '抓取维基百科',
      output: '总结完成',
      status: 'completed',
    })
  })

  it('acp_agent 映射为 SubAgentRef，合并 summary 与 output', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: {
          tool_name: 'acp_agent',
          tool_call_id: 'acp_1',
          arguments: { task: '实现登录', provider: 'acp.opencode' },
        },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.result',
        data: {
          tool_name: 'acp_agent',
          tool_call_id: 'acp_1',
          result: { status: 'completed', summary: '已完成', output: '详见文件 x.go' },
        },
      }),
    ]
    const agents = buildMessageSubAgentsFromRunEvents(events)
    expect(agents).toHaveLength(1)
    expect(agents[0]).toMatchObject({
      id: 'acp_1',
      sourceTool: 'acp_agent',
      input: '实现登录',
      personaId: 'acp.opencode',
      status: 'completed',
      output: '已完成\n\n详见文件 x.go',
    })
  })

  it('应识别 spawn_acp 与 wait_acp，并把 handle 输出收敛到同一个 ACP 条目', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: {
          tool_name: 'spawn_acp',
          tool_call_id: 'call_spawn_acp',
          arguments: { task: '实现登录', provider: 'acp.opencode' },
        },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.result',
        data: {
          tool_name: 'spawn_acp',
          tool_call_id: 'call_spawn_acp',
          result: { handle_id: 'handle_1', status: 'running' },
        },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 3,
        type: 'tool.result',
        data: {
          tool_name: 'wait_acp',
          tool_call_id: 'call_wait_acp',
          result: { handle_id: 'handle_1', status: 'completed', output: '任务完成' },
        },
      }),
    ]

    const agents = buildMessageSubAgentsFromRunEvents(events)
    expect(agents).toHaveLength(1)
    expect(agents[0]).toMatchObject({
      id: 'call_spawn_acp',
      sourceTool: 'acp_agent',
      subAgentId: 'handle_1',
      input: '实现登录',
      personaId: 'acp.opencode',
      output: '任务完成',
      status: 'completed',
    })
  })

  it('应识别 close_acp 并更新已有 ACP 条目的状态', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'tool.call',
        data: {
          tool_name: 'spawn_acp',
          tool_call_id: 'call_spawn_acp',
          arguments: { task: '检查仓库', provider: 'acp.opencode' },
        },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'tool.result',
        data: {
          tool_name: 'spawn_acp',
          tool_call_id: 'call_spawn_acp',
          result: { handle_id: 'handle_2', status: 'running' },
        },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 3,
        type: 'tool.result',
        data: {
          tool_name: 'close_acp',
          tool_call_id: 'call_close_acp',
          result: { handle_id: 'handle_2', status: 'closed' },
        },
      }),
    ]

    const agents = buildMessageSubAgentsFromRunEvents(events)
    expect(agents).toHaveLength(1)
    expect(agents[0]).toMatchObject({
      id: 'call_spawn_acp',
      subAgentId: 'handle_2',
      status: 'closed',
      sourceTool: 'acp_agent',
    })
  })
})

describe('shouldReplayMessageCodeExecutions', () => {
  it('shell 记录缺少 sessionId 时应触发回放修复', () => {
    expect(shouldReplayMessageCodeExecutions([{
      id: 'call_exec',
      language: 'shell',
      code: 'uname -a',
      output: 'Linux',
      status: 'success',
    }])).toBe(true)
  })

  it('空数组哨兵不应重复触发回放', () => {
    expect(shouldReplayMessageCodeExecutions([])).toBe(false)
  })

  it('已有 sessionId 的 shell 记录不需要额外回放', () => {
    expect(shouldReplayMessageCodeExecutions([{
      id: 'call_exec',
      language: 'shell',
      code: 'uname -a',
      output: 'Linux',
      sessionId: 'sess_1',
      status: 'success',
    }])).toBe(false)
  })
})

describe('patchCodeExecutionList', () => {
  it('同一 shell session 下更新后续命令时，不应覆盖之前的命令', () => {
    const executions = [
      {
        id: 'call_exec_1',
        language: 'shell' as const,
        code: 'pwd',
        output: '/tmp',
        sessionId: 'sess_1',
        status: 'success' as const,
      },
      {
        id: 'call_exec_2',
        language: 'shell' as const,
        code: 'ls',
        sessionId: 'sess_1',
        status: 'running' as const,
      },
    ]

    const result = patchCodeExecutionList(executions, {
      id: 'call_exec_2',
      language: 'shell',
      code: 'ls',
      output: 'a.txt',
      exitCode: 0,
      sessionId: 'sess_1',
      status: 'success',
    })

    expect(result.next).toEqual([
      {
        id: 'call_exec_1',
        language: 'shell',
        code: 'pwd',
        output: '/tmp',
        sessionId: 'sess_1',
        status: 'success',
      },
      {
        id: 'call_exec_2',
        language: 'shell',
        code: 'ls',
        output: 'a.txt',
        exitCode: 0,
        sessionId: 'sess_1',
        status: 'success',
      },
    ])
  })
})

describe('buildMessageThinkingFromRunEvents', () => {
  it('应提取顶层 thinking 文本', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'message.delta',
        data: { role: 'assistant', channel: 'thinking', content_delta: 'A' },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'message.delta',
        data: { role: 'assistant', channel: 'thinking', content_delta: 'B' },
      }),
    ]

    const snapshot = buildMessageThinkingFromRunEvents(events)
    expect(snapshot).not.toBeNull()
    expect(snapshot?.thinkingText).toBe('AB')
    expect(snapshot?.segments).toEqual([])
  })

  it('应提取 segment 文本并过滤 hidden 段', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'run.segment.start',
        data: { segment_id: 'seg_1', kind: 'planning_round', display: { mode: 'collapsed', label: 'Plan' } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'message.delta',
        data: { role: 'assistant', content_delta: 'P1' },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 3,
        type: 'run.segment.end',
        data: { segment_id: 'seg_1' },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 4,
        type: 'run.segment.start',
        data: { segment_id: 'seg_2', kind: 'planning_round', display: { mode: 'hidden', label: 'Hidden' } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 5,
        type: 'message.delta',
        data: { role: 'assistant', content_delta: 'H1' },
      }),
    ]

    const snapshot = buildMessageThinkingFromRunEvents(events)
    expect(snapshot).not.toBeNull()
    expect(snapshot?.segments).toHaveLength(1)
    expect(snapshot?.segments[0]).toMatchObject({
      segmentId: 'seg_1',
      label: 'Plan',
      content: 'P1',
    })
  })

  it('没有 thinking 内容时应返回 null', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'message.delta',
        data: { role: 'assistant', content_delta: 'Final answer' },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'run.completed',
      }),
    ]

    const snapshot = buildMessageThinkingFromRunEvents(events)
    expect(snapshot).toBeNull()
  })

  it('segment.start 缺少 segment_id 时跳过', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'run.segment.start',
        data: { kind: 'planning_round', display: { mode: 'collapsed', label: 'NoID' } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'message.delta',
        data: { role: 'assistant', content_delta: 'orphaned delta' },
      }),
    ]

    const snapshot = buildMessageThinkingFromRunEvents(events)
    expect(snapshot).toBeNull()
  })

  it('非 assistant role 的 message.delta 被过滤', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'run.segment.start',
        data: { segment_id: 'seg_1', kind: 'planning_round', display: { mode: 'collapsed', label: 'Plan' } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'message.delta',
        data: { role: 'user', content_delta: 'user message' },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 3,
        type: 'message.delta',
        data: { role: 'assistant', content_delta: 'valid' },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 4,
        type: 'run.segment.end',
        data: { segment_id: 'seg_1' },
      }),
    ]

    const snapshot = buildMessageThinkingFromRunEvents(events)
    expect(snapshot).not.toBeNull()
    expect(snapshot?.segments[0].content).toBe('valid')
  })

  it('空 content_delta 被过滤', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'message.delta',
        data: { role: 'assistant', channel: 'thinking', content_delta: '' },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'message.delta',
        data: { role: 'assistant', channel: 'thinking', content_delta: 'real' },
      }),
    ]

    const snapshot = buildMessageThinkingFromRunEvents(events)
    expect(snapshot).not.toBeNull()
    expect(snapshot?.thinkingText).toBe('real')
  })

  it('segment.end 的 segment_id 不匹配当前活跃 segment 时不关闭', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'run.segment.start',
        data: { segment_id: 'seg_1', kind: 'planning_round', display: { mode: 'collapsed', label: 'Plan' } },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 2,
        type: 'run.segment.end',
        data: { segment_id: 'seg_wrong' },
      }),
      makeRunEvent({
        runId: 'run_1',
        seq: 3,
        type: 'message.delta',
        data: { role: 'assistant', content_delta: 'still in seg_1' },
      }),
    ]

    const snapshot = buildMessageThinkingFromRunEvents(events)
    expect(snapshot).not.toBeNull()
    expect(snapshot?.segments).toHaveLength(1)
    expect(snapshot?.segments[0].content).toBe('still in seg_1')
  })

  it('content_delta 非 string 类型被过滤', () => {
    const events = [
      makeRunEvent({
        runId: 'run_1',
        seq: 1,
        type: 'message.delta',
        data: { role: 'assistant', channel: 'thinking', content_delta: 123 },
      }),
    ]

    const snapshot = buildMessageThinkingFromRunEvents(events)
    expect(snapshot).toBeNull()
  })
})

describe('load_tools summary', () => {
  it('returns state-aware description when statuses exist', () => {
    const result = fileOpOutputFromResult('load_tools', {
      count: 3,
      matched: [
        { name: 'show_widget', state: 'loaded' },
        { name: 'create_artifact', already_loaded: true },
        { name: 'memory_search', already_active: true },
      ],
    })
    expect(result).toBe('loaded 1 (show_widget); already loaded 1 (create_artifact); already active 1 (memory_search)')
  })

  it('falls back to name list when no state info is present', () => {
    const result = fileOpOutputFromResult('load_tools', {
      count: 2,
      matched: ['web_search', 'web_fetch'],
    })
    expect(result).toBe('2 matches: web_search, web_fetch')
  })
})

describe('memory_search file result summary', () => {
  it('lists abstracts from hits', () => {
    expect(
      fileOpOutputFromResult('memory_search', {
        hits: [{ uri: 'a', abstract: '第一行摘要' }, { uri: 'b', abstract: '第二行' }],
      }),
    ).toBe('2 results\n第一行摘要\n第二行')
  })

  it('falls back to uri when abstract missing', () => {
    expect(
      fileOpOutputFromResult('memory_search', {
        hits: [{ uri: 'local://memory/abc' }],
      }),
    ).toBe('1 result\nlocal://memory/abc')
  })

  it('still accepts legacy results key', () => {
    expect(
      fileOpOutputFromResult('memory_search', {
        results: [{ abstract: 'legacy' }],
      }),
    ).toBe('1 result\nlegacy')
  })

  it('head only when legacy hit has no fields', () => {
    expect(
      fileOpOutputFromResult('memory_search', {
        results: [{}],
      }),
    ).toBe('1 result')
  })

  it('reports no results when both missing or empty', () => {
    expect(fileOpOutputFromResult('memory_search', {})).toBe('(no results)')
    expect(fileOpOutputFromResult('memory_search', { hits: [] })).toBe('(no results)')
  })

  it('caps listed lines and shows remainder count', () => {
    const hits = Array.from({ length: 45 }, (_, i) => ({ abstract: `h${i}` }))
    const out = fileOpOutputFromResult('memory_search', { hits })
    expect(out?.startsWith('45 results\n')).toBe(true)
    expect(out).toContain('h0')
    expect(out).toContain('h39')
    expect(out).not.toContain('h40')
    expect(out?.endsWith('\n… 5 more')).toBe(true)
  })
})
