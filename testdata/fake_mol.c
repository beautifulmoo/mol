/*
 * fake_mol — 업데이트 실패·롤백 테스트용 가짜 mol 바이너리.
 *
 * - --version / -version: "mol 0.0.0" 출력 후 0 종료 → 업로드 검증 통과
 * - 그 외(실제 서비스 기동): 1 종료 → systemctl start 실패 → update.sh 롤백
 *
 * 빌드: gcc -o fake_mol fake_mol.c && strip fake_mol
 * 테스트: ./fake_mol --version  # mol 0.0.0
 *         ./fake_mol            # exit 1
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main(int argc, char **argv) {
	if (argc >= 2) {
		if (strcmp(argv[1], "--version") == 0 || strcmp(argv[1], "-version") == 0) {
			puts("mol 0.0.0");
			return 0;
		}
	}
	return 1;
}
