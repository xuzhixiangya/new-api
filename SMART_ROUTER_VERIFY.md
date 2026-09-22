# 智能路由验证结果

验证时间：2026-09-21 18:53
本轮标记：`smart-router-final-1789987978`

明天打开后台看效果：
- 系统设置 → 模型 → 智能路由（便宜/中间/顶尖已配置）
- 使用日志：看「原始模型 auto → 路由模型」
- 调用日志：看完整对话和实际模型

## 本轮新补的场景

| 场景 | 结果 | 落到 | 档位 | 原因 | 请求 ID |
|---|---|---|---|---|---|
| 周报-人事行政 | 通过 | `deepseek/deepseek-v4.1-flash` | cheap | last_turn | `20260921105258804142000VgA8JSY1UAs7cVXP` |
| 写脚本后再改一下-不应升顶尖 | 通过 | `openai/gpt-5.4` | mid | Simple revision request, but lacks context; mid is safe default | `20260921105307480424000VgA8JSY16NBYCJSB` |
| 架构后再改一下-应继承顶尖 | 通过 | `openai/gpt-5.5` | strong | continuation | `20260921105314447105000VgA8JSY18AX51RL1` |
| 翻译加分布式-难题优先 | 通过 | `openai/gpt-5.5` | strong | hard_keyword | `20260921105319389147000VgA8JSY1xVrU5XFS` |
| Responses协议-简单翻译 | 通过 | `deepseek/deepseek-v4.1-flash` | cheap | easy_keyword | `20260921105326207627000VgA8JSY1oDVj05vX` |
| 长合同粘贴审阅 | 通过 | `openai/gpt-5.5` | strong | last_turn | `20260921105328729951000VgA8JSY1v7C7y7vS` |
| 三个工具保底中间档 | 通过 | `openai/gpt-5.4` | mid | easy_keyword | `20260921105335707858000VgA8JSY1TWdFFJ1c` |

## 之前已经验证过的效果

| 场景 | 结果 | 落到 | 说明 |
|---|---|---|---|
| 简单翻译 | 通过 | `deepseek/deepseek-v4.1-flash` | 本地 easy，便宜档 |
| 写脚本（拿不准） | 通过 | `openai/gpt-5.4` | 分类器拿不准回落 mid |
| 分布式架构 | 通过 | `openai/gpt-5.5` | 本地 hard |
| 固定模型透传 | 通过 | `deepseek/deepseek-v4.1-flash` | 不改模型，无 smart_router 字段 |
| 架构后继续写 | 通过 | `openai/gpt-5.5` | continuation=true |
| 架构后问天气 | 通过 | `deepseek/deepseek-v4.1-flash` | 换题 |
| 请假理由 | 通过 | `deepseek/deepseek-v4.1-flash` | 人事短任务 |
| 实现 hello world | 通过 | `deepseek/deepseek-v4.1-flash` | 不再误升顶尖，分类器判简单 |
| 设计生日文案 | 通过 | `openai/gpt-5.4` | 「设计」不再直接顶尖 |
| 架构后谢谢 | 通过 | `deepseek/deepseek-v4.1-flash` | 致谢不继承 |
| 架构后总结一下 | 通过 | `openai/gpt-5.4` | 基于上文总结保中间档 |
| 架构后为什么 | 通过 | `openai/gpt-5.5` | 追问继承 |
| JSON Schema 翻译 | 通过 | `openai/gpt-5.4` | 协议保底中间档 |

## 这轮优化了什么

1. 「实现 / 设计」日常说法不再直接走顶尖。
2. 多轮「谢谢 / 好的」不再继承难题。
3. 「总结一下」有长上文时至少走中间档。
4. 普通写脚本后再说「再改一下」，不再因为代码块抬到顶尖。
5. 「翻译 + 分布式」这类混合句，难题优先。
6. 分类器最少等 2 秒，并能解析 markdown / reasoning 里的 JSON。

## 怎么在后台核对

使用日志按时间看今晚 18:35 之后的 `auto` 记录，悬停模型列应显示原始模型 → 路由模型。
调用日志里同一条请求应有助手回复。

看图保底中间档、强制路由用户，单测已覆盖，今晚没再打真实上游（避免改你的用户开关）。

本轮失败数：0

## 多轮补测（今早）

| 场景 | 结果 | 落到 | 档位 | 续写 | 请求 ID |
|---|---|---|---|---|---|
| 架构后请假-应换题走便宜 | 通过 | `deepseek/deepseek-v4.1-flash` | cheap | False | `202609220113329629710007sCNvh7Yhs4a1HTH` |
| 架构后明天天气-应换题走便宜 | 通过 | `deepseek/deepseek-v4.1-flash` | cheap | False | `202609220113378611510007sCNvh7YoMpcFr8C` |
| 架构后你好-不应继承顶尖 | 通过 | `deepseek/deepseek-v4.1-flash` | cheap | False | `202609220113392835890007sCNvh7YVE0xS3ec` |
| 架构后好的继续-应继承顶尖 | 通过 | `openai/gpt-5.5` | strong | True | `202609220113405365940007sCNvh7YEoCiKcus` |
| 架构后提取要点-中间档 | 通过 | `openai/gpt-5.4` | mid | False | `202609220113452893960007sCNvh7YY13DdLeK` |
| 拒绝邮件后改语气-便宜续写 | 通过 | `deepseek/deepseek-v4.1-flash` | cheap | True | `202609220113468710310007sCNvh7YUpr7ErfA` |
| 翻译后改问架构-应走顶尖 | 通过 | `openai/gpt-5.5` | strong | False | `202609220113481573750007sCNvh7YNmgYrBZG` |
| 三轮谢谢后再问为什么-继承顶尖 | 通过 | `openai/gpt-5.5` | strong | True | `202609220113528832580007sCNvh7YdgnWokOx` |
| 写脚本后换成Python-中间档 | 通过 | `openai/gpt-5.4` | mid | True | `202609220113561939910007sCNvh7YZnJsdXJo` |

这轮新发现：架构聊完后说「帮我写个请假理由」或「你好」，以前会误继承顶尖。现已改成换题/问候，走便宜档。
本轮失败数：0
