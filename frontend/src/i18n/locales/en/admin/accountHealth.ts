export default {
  accountHealth: {
    title: 'Account Health',
    description: 'Live OpenAI account health, automatic quarantine, and scheduler recovery',
    actions: { refresh: 'Refresh', run: 'Run now', save: 'Apply settings' },
    summary: {
      total: 'Total', eligible: 'Scheduler eligible', healthy: 'Tested healthy', degraded: 'Retrying',
      recovering: 'Recovering', blocked: 'Quarantined', unknown: 'Untested', inFlight: 'In flight'
    },
    settings: {
      title: 'Health controller', subtitle: 'Changes are applied within about 2 seconds without a restart', enabled: 'Enable checks',
      autoRecover: 'Auto recover scheduling', autoBlock: 'Auto quarantine failures', workers: 'Concurrent workers', batchSize: 'Lease batch size',
      autoAssignGroup: 'Auto-assign accounts', targetGroup: 'Target OpenAI group', targetGroupPlaceholder: 'Select a group',
      dispatchInterval: 'Dispatch interval (s)', healthyInterval: 'Healthy interval (s)', recoveryInterval: 'Retry interval (s)',
      failureThreshold: 'Failure threshold', successThreshold: 'Recovery threshold', timeout: 'Probe timeout (s)',
      model: 'Test model', modelHint: 'Leave empty to use the system default test model'
    },
    filters: { search: 'Search accounts', allStates: 'All health states' },
    table: {
      account: 'Account', health: 'Tested health', scheduler: 'Scheduler', failures: 'Failures', reason: 'Latest result',
      lastProbe: 'Last probe', nextProbe: 'Next probe', latency: 'Latency', eligible: 'Eligible', unavailable: 'Unavailable',
      never: 'Not tested', noError: 'Connection healthy'
    },
    states: { unknown: 'Untested', healthy: 'Healthy', degraded: 'Degraded', recovering: 'Recovering', blocked: 'Quarantined' },
    messages: { loadFailed: 'Failed to load account health', saveSuccess: 'Health settings applied', runQueued: '{count} accounts queued for health checks' }
  }
}
