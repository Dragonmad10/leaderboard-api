#!/bin/bash
# Test script for Leaderboard API
# Uso: ./test_api.sh

set -e

BASE_URL="http://localhost:8080"
LEADERBOARD_ID="game-1"
USER_ID="player-1"

echo "🧪 Testing Leaderboard API"
echo "=========================="

# 1. Health check
echo -e "\n1️⃣  Health Check"
curl -s "$BASE_URL/health" | jq .

# 2. Create user
echo -e "\n2️⃣  Create User"
curl -s -X POST "$BASE_URL/api/v1/users" \
  -H "Content-Type: application/json" \
  -d '{"user_id": "'$USER_ID'", "username": "Marco"}' | jq .

# 3. Create leaderboard
echo -e "\n3️⃣  Create Leaderboard"
curl -s -X POST "$BASE_URL/api/v1/leaderboards" \
  -H "Content-Type: application/json" \
  -d '{"leaderboard_id": "'$LEADERBOARD_ID'", "name": "Top Scores"}' | jq .

# 4. Submit score (POST /scores)
echo -e "\n4️⃣  Submit Score (absolute)"
curl -s -X POST "$BASE_URL/api/v1/leaderboards/$LEADERBOARD_ID/scores" \
  -H "Content-Type: application/json" \
  -d '{"user_id": "'$USER_ID'", "score": 1500}' | jq .

# 5. Submit another user's score
echo -e "\n5️⃣  Submit Another User Score"
curl -s -X POST "$BASE_URL/api/v1/leaderboards/$LEADERBOARD_ID/scores" \
  -H "Content-Type: application/json" \
  -d '{"user_id": "player-2", "score": 2000}' | jq .

# 6. Increment score (POST /scores/increment)
echo -e "\n6️⃣  Increment Score (delta)"
curl -s -X POST "$BASE_URL/api/v1/leaderboards/$LEADERBOARD_ID/scores/increment" \
  -H "Content-Type: application/json" \
  -d '{"user_id": "'$USER_ID'", "delta": 500}' | jq .

# 7. Get top scores (GET /scores)
echo -e "\n7️⃣  Get Top Scores (limit=10)"
curl -s "$BASE_URL/api/v1/leaderboards/$LEADERBOARD_ID/scores?limit=10&offset=0" | jq .

# 8. Get user rank (GET /rank)
echo -e "\n8️⃣  Get User Rank"
curl -s "$BASE_URL/api/v1/leaderboards/$LEADERBOARD_ID/rank?user_id=$USER_ID" | jq .

# 9. Test pagination
echo -e "\n9️⃣  Test Pagination (limit=1, offset=0)"
curl -s "$BASE_URL/api/v1/leaderboards/$LEADERBOARD_ID/scores?limit=1&offset=0" | jq .

# 10. Test error handling - invalid JSON
echo -e "\n🔟  Test Error: Invalid JSON"
curl -s -X POST "$BASE_URL/api/v1/leaderboards/$LEADERBOARD_ID/scores" \
  -H "Content-Type: application/json" \
  -d '{"user_id":}' | jq .

# 11. Test error handling - missing required field
echo -e "\n1️⃣1️⃣  Test Error: Missing required field"
curl -s -X POST "$BASE_URL/api/v1/leaderboards/$LEADERBOARD_ID/scores" \
  -H "Content-Type: application/json" \
  -d '{"score": 100}' | jq .

# 12. Test error handling - user not found (when NOT auto-created)
# This would require disabling auto-create, but let's test with a fresh leaderboard
echo -e "\n1️⃣2️⃣  Test Error: Leaderboard not found (404)"
curl -s "$BASE_URL/api/v1/leaderboards/nonexistent/scores?limit=10" | jq .

# 13. Delete user score
echo -e "\n1️⃣3️⃣  Delete User Score"
curl -s -X DELETE "$BASE_URL/api/v1/leaderboards/$LEADERBOARD_ID/scores/$USER_ID" | jq .

# 14. Verify deletion
echo -e "\n1️⃣4️⃣  Verify Deletion (should be empty or only player-2)"
curl -s "$BASE_URL/api/v1/leaderboards/$LEADERBOARD_ID/scores?limit=10" | jq .

echo -e "\n✅ All tests completed!"