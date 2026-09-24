# IFD 路标室：TIFF/GeoTIFF 目录图、块数据与地理变换证据

本目录是仓库自带的离线领域案例包，面向影像数据团队的完整叙事：
导入 TIFF 字节、多级 IFD、tile/strip 与 GeoKey，审查像素存储与地理参考的来源链。

## 样例

| 文件 | 场景 |
| --- | --- |
| baseline-landmark.tiff | 基线：classic 小端，IFD 链 + SubIFD，tile 范围、GeoKey v1.1.0、Tiepoint/Scale 完整保留 |
| baseline-landmark-v2.tiff | 局部源改动：仅 tile 字节数变化（引入重叠），用于验证依赖过期范围 |
| bigtiff-wide-offsets.tiff | BigTIFF 大端，8 字节宽偏移与 LONG8 原值 |
| ifd-cycle.tiff | IFD 链成环，解析器必须靠访问预算终止 |
| shared-subdir.tiff | 两个父 IFD 共享同一个 SubIFD |
| tile-overlap.tiff | tile 范围重叠 + 缺失块，显式呈现不拼凑 |
| geokey-conflict.tiff | ModelTiepoint/Scale 与 ModelTransformation 冲突，双候选保留 |
| budget-exhaustion.tiff | 12 级 IFD 链，配合预算 4 触发访问预算耗尽 |

## 叙事步骤（case.json）

1. 导入基线并核对七项保留清单（大小端、classic/BigTIFF、tag 原值、外部值偏移、IFD 链、tile/strip 范围、GeoKey 版本）。
2. 参数修订：IFD 访问预算 256 → 4，派生结果按参数指纹过期重算。
3. 局部源数据改动：baseline-landmark-v2 仅改 tile 字节数；块数据派生过期，地理候选因指纹不变而存活。
4. 冲突人工决定：两位审阅者分别接受 geokey-conflict 的不同变换候选，随后撤销其一；采纳与未采纳结果都保留。

重放记录实际经过：大小端、BigTIFF 宽偏移、IFD 循环、共享子目录、tile 重叠、
GeoKey 冲突与访问预算耗尽。每个显示值都带稳定身份（ifd@偏移、tag ID、blk ID、
cand ID、evt 序号），导出-删除-再导入后身份、顺序与来源关系不变。

本目录文件由 `go run ./cmd/gencase` 确定性生成；`go test ./internal/casepack` 校验其与代码一致。
