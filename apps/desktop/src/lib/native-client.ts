import { APIClient } from "@router-forge/api";
import { isMyGo } from "mygo-runtime";
import { Console, type Connection } from "../mygo.generated";

export type DesktopConnection = { api: APIClient; connection: Connection };

export async function connectDesktop(): Promise<DesktopConnection> {
  if (!isMyGo()) throw new Error("未检测到 MyGo 原生桥接。请使用 bun run dev:desktop 或打包的 Router Forge 桌面程序；普通浏览器不能控制评测。");
  const connection = await Console.connect();
  const api = new APIClient(connection.url, "", (method, path, body) => Console.request(method, path, body));
  return { api, connection };
}
