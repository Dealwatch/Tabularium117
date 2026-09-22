// Group by session identity; keep unknown regions and all island IDs intact.
export function groupIslands(islands) {
  const groups = new Map();
  for (const island of islands) {
    const key = island.sessionGuid ?? island.sessionName ?? "unknown";
    if (!groups.has(key)) groups.set(key, {
      name: island.sessionName || `#${key}`, islands: [],
    });
    groups.get(key).islands.push(island);
  }
  return [...groups.values()];
}
