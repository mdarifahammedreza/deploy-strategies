// "color-backend-blue-7d9f8c6b5-x2kq9" → "blue-7d9f8c6b5-x2kq9"
// The middle part is the ReplicaSet hash: it changes on every rollout, which is worth pointing out in class.
export function shortName(podName: string): string {
  return podName.replace(/^color-backend-/, '');
}
