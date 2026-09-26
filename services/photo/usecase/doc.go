// Package usecase は photo のユースケース（トランザクション境界）。
// コマンドはEntity + Repository経由、クエリはRead Model直行（CQS）。
// Repository interface と整合性クラス（Atomic / Eventual）の語彙はこの層に定義する。
package usecase
