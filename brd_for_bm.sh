#!/bin/bash

echo "=== Real BM-usable broadcast addresses ==="

for iface in $(ls /sys/class/net); do
    # loopback 제외
    [ "$iface" = "lo" ] && continue

    # 인터페이스 타입 확인
    type=$(cat /sys/class/net/$iface/type 2>/dev/null)

    # type=1 은 Ethernet (물리 NIC / VLAN / bond / bridge)
    [ "$type" != "1" ] && continue

    # 브리지인 경우 → 실제 슬레이브(물리 NIC 등)가 있는지 확인
    if [ -d /sys/class/net/$iface/brif ]; then
        slaves=$(ls /sys/class/net/$iface/brif | wc -l)
        if [ "$slaves" -eq 0 ]; then
            # 물리 NIC 등과 연결되지 않은 내부 전용 브리지 → 제외
            continue
        fi
    fi

    # 이 인터페이스의 모든 IPv4 주소 라인 가져오기
    mapfile -t lines < <(ip -4 -o addr show dev "$iface")

    [ ${#lines[@]} -eq 0 ] && continue

    # 이 인터페이스에서 중복되지 않는 brd 목록 생성
    declare -A seen_brd=()
    brd_list=()

    for line in "${lines[@]}"; do
        brd=$(echo "$line" | awk '/brd/ {print $6}')

        [ -z "$brd" ] && continue

        if [ -z "${seen_brd[$brd]}" ]; then
            seen_brd[$brd]=1
            brd_list+=("$brd")
        fi
    done

    # 이 인터페이스에서 brd 가 하나라도 있었다면 출력
    if [ ${#brd_list[@]} -gt 0 ]; then
        for br in "${brd_list[@]}"; do
            echo "$iface : $br"
        done
    fi

    unset seen_brd
done
