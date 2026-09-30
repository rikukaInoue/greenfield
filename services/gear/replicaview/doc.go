// Package replicaview は他ドメインの公開データの読み取り専用の複製（ReplicaView）。
// 正は相手にあり、イベント購読で追従する。表示専用であり業務判断に使わない。
// domain / usecase からこのパッケージを import することは depguard で禁止している（.golangci.yml）。
package replicaview
