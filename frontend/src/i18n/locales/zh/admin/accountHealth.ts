export default {
  accountHealth: {
    title: '账号巡检',
    description: 'OpenAI 账号实时健康、自动隔离与恢复调度',
    actions: { refresh: '刷新', run: '立即巡检', save: '热更新设置' },
    summary: {
      total: '账号总数', eligible: '调度可用', healthy: '实测健康', degraded: '异常重试',
      recovering: '恢复中', blocked: '已隔离', unknown: '未巡检', inFlight: '正在巡检'
    },
    settings: {
      title: '巡检控制器', subtitle: '保存后约 2 秒内热更新，无需重启容器', enabled: '启用巡检',
      autoRecover: '自动恢复调度', autoBlock: '失败自动隔离', workers: '并发 Worker', batchSize: '单批取号',
      autoAssignGroup: '账号自动入组', targetGroup: '目标 OpenAI 分组', targetGroupPlaceholder: '选择分组',
      dispatchInterval: '调度间隔（秒）', healthyInterval: '健康复检（秒）', recoveryInterval: '异常复检（秒）',
      failureThreshold: '失败阈值', successThreshold: '恢复成功阈值', timeout: '单号超时（秒）',
      model: '测试模型', modelHint: '留空时使用系统默认测试模型'
    },
    filters: { search: '搜索账号', allStates: '全部健康状态' },
    table: {
      account: '账号', health: '实测健康', scheduler: '调度状态', failures: '连续失败', reason: '最近结果',
      lastProbe: '最近巡检', nextProbe: '下次巡检', latency: '延迟', eligible: '可调度', unavailable: '不可调度',
      never: '尚未巡检', noError: '连接正常'
    },
    states: { unknown: '未巡检', healthy: '健康', degraded: '异常', recovering: '恢复中', blocked: '已隔离' },
    messages: { loadFailed: '加载巡检数据失败', saveSuccess: '巡检设置已热更新', runQueued: '已将 {count} 个账号加入巡检队列' }
  }
}
